package cobra_test

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	imp "github.com/go-rotini/import"
	cobraimport "github.com/go-rotini/import/cobra"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func run(*cobra.Command, []string) {}

// modernCobra reports whether the Cobra under test is 1.8 or later, the first version that
// exposes flag completion functions.
func modernCobra() bool {
	_, ok := any(&cobra.Command{}).(interface {
		GetFlagCompletionFunc(string) (func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective), bool)
	})
	return ok
}

func importTree(t *testing.T, root *cobra.Command, opts cobraimport.Options) *imp.Result {
	t.Helper()
	res, err := cobraimport.FromCobra(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	// Every result must survive the description's own round trip.
	var buf bytes.Buffer
	if err := imp.Write(&buf, res); err != nil {
		t.Fatal(err)
	}
	if _, err := imp.Read(&buf); err != nil {
		t.Fatal(err)
	}
	return res
}

func find(t *testing.T, res *imp.Result, path string) *imp.Command {
	t.Helper()
	parts := strings.Fields(path)
	c := res.Command
	if c.Name != parts[0] {
		t.Fatalf("root is %q, not %q", c.Name, parts[0])
	}
next:
	for _, p := range parts[1:] {
		for _, sub := range c.Commands {
			if sub.Name == p {
				c = sub
				continue next
			}
		}
		t.Fatalf("no command %q", path)
	}
	return c
}

func flagNamed(t *testing.T, c *imp.Command, name string) *imp.Flag {
	t.Helper()
	for _, f := range c.Flags {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("%s has no flag %q", c.Name, name)
	return nil
}

func hasFlag(c *imp.Command, name string) bool {
	for _, f := range c.Flags {
		if f.Name == name {
			return true
		}
	}
	return false
}

// notesFor returns the notes on path whose message contains substr.
func notesFor(res *imp.Result, path, substr string) []imp.Note {
	var out []imp.Note
	for _, n := range res.Notes {
		if n.Path == path && strings.Contains(n.Msg, substr) {
			out = append(out, n)
		}
	}
	return out
}

func wantNote(t *testing.T, res *imp.Result, path string, level imp.Level, substr string) {
	t.Helper()
	ns := notesFor(res, path, substr)
	if len(ns) != 1 {
		t.Fatalf("want one note on %q containing %q, got %d:\n%s", path, substr, len(ns), dumpNotes(res))
	}
	if ns[0].Level != level {
		t.Fatalf("note %q is %s, want %s", ns[0].Msg, ns[0].Level, level)
	}
}

func noNote(t *testing.T, res *imp.Result, path, substr string) {
	t.Helper()
	if ns := notesFor(res, path, substr); len(ns) > 0 {
		t.Fatalf("unexpected note on %q: %v", path, ns)
	}
}

func dumpNotes(res *imp.Result) string {
	var b strings.Builder
	for _, n := range res.Notes {
		fmt.Fprintln(&b, n)
	}
	return b.String()
}

func argSummary(c *imp.Command) string {
	var parts []string
	for _, a := range c.Arguments {
		s := a.Name + ":" + a.Schema.Type
		if a.Schema.Required {
			s += "!"
		}
		if a.Schema.MinItems != nil {
			s += fmt.Sprintf(">=%d", *a.Schema.MinItems)
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}

func TestFromCobraNil(t *testing.T) {
	if _, err := cobraimport.FromCobra(nil, cobraimport.Options{}); err == nil {
		t.Fatal("want an error")
	}
}

func TestImportStartsAtRoot(t *testing.T) {
	root := &cobra.Command{Use: "app"}
	child := &cobra.Command{Use: "child", Run: run}
	root.AddCommand(child)
	res := importTree(t, child, cobraimport.Options{})
	if res.Command.Name != "app" {
		t.Fatalf("root %q", res.Command.Name)
	}
	if res.Source.Framework != "cobra" || res.Source.Version == "" {
		t.Fatalf("source %+v", res.Source)
	}
}

func TestBuiltinDefaults(t *testing.T) {
	root := &cobra.Command{Use: "app", Version: "1.2.3"}
	child := &cobra.Command{Use: "child", Run: run}
	root.AddCommand(child)
	res := importTree(t, root, cobraimport.Options{})

	help := res.Command.Flags[0]
	if help.Builtin != "help" || !help.Cascading || !help.ShortCircuit ||
		!reflect.DeepEqual(help.Identifiers, []string{"-h", "--help"}) {
		t.Fatalf("help flag %+v", help)
	}
	version := flagNamed(t, res.Command, "version")
	if version.Builtin != "version" || version.Cascading || !version.ShortCircuit ||
		!reflect.DeepEqual(version.Identifiers, []string{"-v", "--version"}) {
		t.Fatalf("version flag %+v", version)
	}
	wantNote(t, res, "app", imp.Info, `Version "1.2.3"`)
	if hasFlag(find(t, res, "app child"), "help") {
		t.Fatal("a sub-command repeats the help flag")
	}

	h := find(t, res, "app help")
	if h.Builtin != "help" || argSummary(h) != "command:[]string" {
		t.Fatalf("help command %+v %s", h, argSummary(h))
	}
	comp := find(t, res, "app completion")
	if comp.Builtin != "completion" || argSummary(comp) != "shell:string!" || len(comp.Commands) != 0 {
		t.Fatalf("completion command %+v", comp)
	}
	var shells []string
	for _, e := range comp.Arguments[0].Schema.Enum {
		shells = append(shells, e.Value)
	}
	if !reflect.DeepEqual(shells, []string{"bash", "fish", "powershell", "zsh"}) {
		t.Fatalf("shells %v", shells)
	}
	if !hasFlag(comp, "no-descriptions") {
		t.Fatal("completion lost --no-descriptions")
	}
	wantNote(t, res, "app completion", imp.Info, "merged into one command")
}

func TestBuiltinVariants(t *testing.T) {
	t.Run("version shorthand taken", func(t *testing.T) {
		root := &cobra.Command{Use: "app", Version: "1", Run: run}
		root.Flags().BoolP("verbose", "v", false, "")
		res := importTree(t, root, cobraimport.Options{})
		if ids := flagNamed(t, res.Command, "version").Identifiers; !reflect.DeepEqual(ids, []string{"--version"}) {
			t.Fatalf("identifiers %v", ids)
		}
	})
	t.Run("no sub-commands", func(t *testing.T) {
		root := &cobra.Command{Use: "app", Run: run}
		res := importTree(t, root, cobraimport.Options{})
		if len(res.Command.Commands) != 0 {
			t.Fatalf("commands %v", res.Command.Commands)
		}
		if hasFlag(res.Command, "version") {
			t.Fatal("version flag without a Version")
		}
	})
	t.Run("completion options", func(t *testing.T) {
		root := &cobra.Command{Use: "app"}
		root.AddCommand(&cobra.Command{Use: "x", Run: run})
		root.CompletionOptions.HiddenDefaultCmd = true
		root.CompletionOptions.DisableDescriptions = true
		res := importTree(t, root, cobraimport.Options{})
		comp := find(t, res, "app completion")
		if !comp.Hidden || hasFlag(comp, "no-descriptions") {
			t.Fatalf("completion %+v", comp)
		}
		wantNote(t, res, "app completion", imp.Info, "DisableDescriptions")
	})
	t.Run("completion disabled", func(t *testing.T) {
		root := &cobra.Command{Use: "app"}
		root.AddCommand(&cobra.Command{Use: "x", Run: run})
		root.CompletionOptions.DisableDefaultCmd = true
		res := importTree(t, root, cobraimport.Options{})
		for _, c := range res.Command.Commands {
			if c.Name == "completion" {
				t.Fatal("completion command imported")
			}
		}
	})
	t.Run("own completion command", func(t *testing.T) {
		root := &cobra.Command{Use: "app"}
		root.AddCommand(&cobra.Command{Use: "completion SHELL", Args: cobra.ExactArgs(1), ValidArgs: []string{"bash", "zsh"}, Run: run})
		res := importTree(t, root, cobraimport.Options{})
		comp := find(t, res, "app completion")
		if comp.Builtin != "" || argSummary(comp) != "shell:string!" || len(comp.Arguments[0].Schema.Enum) != 2 {
			t.Fatalf("completion %+v %s", comp, argSummary(comp))
		}
	})
	t.Run("help shorthand taken", func(t *testing.T) {
		root := &cobra.Command{Use: "app", Run: run}
		// Cobra panics adding its own help flag when -h is taken, so such a program
		// declares its help flag itself.
		root.Flags().Bool("help", false, "show help")
		root.Flags().StringP("host", "h", "", "")
		res := importTree(t, root, cobraimport.Options{})
		if ids := flagNamed(t, res.Command, "help").Identifiers; !reflect.DeepEqual(ids, []string{"--help"}) {
			t.Fatalf("identifiers %v", ids)
		}
		wantNote(t, res, "app", imp.Lossy, "-h belongs to --host")
	})
	t.Run("cobra panics", func(t *testing.T) {
		root := &cobra.Command{Use: "app", Run: run}
		root.Flags().StringP("host", "h", "", "")
		if _, err := cobraimport.FromCobra(root, cobraimport.Options{}); err == nil || !strings.Contains(err.Error(), "panicked") {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("internal commands skipped", func(t *testing.T) {
		root := &cobra.Command{Use: "app"}
		root.AddCommand(&cobra.Command{Use: "__complete", Hidden: true, Run: run}, &cobra.Command{Use: "x", Run: run})
		res := importTree(t, root, cobraimport.Options{})
		for _, c := range res.Command.Commands {
			if strings.HasPrefix(c.Name, "__") || c.Name == "complete" {
				t.Fatalf("imported %q", c.Name)
			}
		}
	})
}

type custom struct{ v string }

func (c *custom) String() string     { return c.v }
func (c *custom) Set(s string) error { c.v = s; return nil }
func (c *custom) Type() string       { return "flavor" }

// typed is a pflag.Value that reports a given type name, standing in for pflag types newer
// than the oldest supported pflag.
type typed struct{ name string }

func (t typed) String() string   { return "" }
func (t typed) Set(string) error { return nil }
func (t typed) Type() string     { return t.name }

func TestFlagTypes(t *testing.T) {
	root := &cobra.Command{Use: "app", Run: run}
	f := root.Flags()
	f.Bool("bool", false, "")
	f.String("string", "", "")
	f.Int("int", 0, "")
	f.Int8("int8", 0, "")
	f.Int16("int16", 0, "")
	f.Int32("int32", 0, "")
	f.Int64("int64", 0, "")
	f.Uint("uint", 0, "")
	f.Uint8("uint8", 0, "")
	f.Uint16("uint16", 0, "")
	f.Uint32("uint32", 0, "")
	f.Uint64("uint64", 0, "")
	f.Float32("float32", 0, "")
	f.Float64("float64", 0, "")
	f.Duration("duration", 0, "")
	f.Count("count", "")
	f.IP("ip", nil, "")
	f.IPNet("ipnet", net.IPNet{}, "")
	f.IPMask("ipmask", nil, "")
	f.BytesHex("hex", nil, "")
	f.BytesBase64("b64", nil, "")
	f.StringSlice("stringslice", nil, "")
	f.StringArray("stringarray", nil, "")
	f.IntSlice("intslice", nil, "")
	f.Int32Slice("int32slice", nil, "")
	f.Int64Slice("int64slice", nil, "")
	f.UintSlice("uintslice", nil, "")
	f.Float32Slice("float32slice", nil, "")
	f.Float64Slice("float64slice", nil, "")
	f.BoolSlice("boolslice", nil, "")
	f.DurationSlice("durationslice", nil, "")
	f.IPSlice("ipslice", nil, "")
	f.StringToString("s2s", nil, "")
	f.StringToInt("s2i", nil, "")
	f.StringToInt64("s2i64", nil, "")
	f.Var(&custom{}, "flavor", "")
	f.Var(typed{"time"}, "when", "")
	f.Var(typed{"func"}, "fn", "")
	f.Var(typed{"boolfunc"}, "bfn", "")
	f.Var(typed{"ipNetSlice"}, "nets", "")
	res := importTree(t, root, cobraimport.Options{})

	want := map[string][2]string{
		"bool": {"bool", ""}, "string": {"string", ""}, "int": {"int", ""}, "int8": {"int8", ""},
		"int16": {"int16", ""}, "int32": {"int32", ""}, "int64": {"int64", ""}, "uint": {"uint", ""},
		"uint8": {"uint8", ""}, "uint16": {"uint16", ""}, "uint32": {"uint32", ""}, "uint64": {"uint64", ""},
		"float32": {"float32", ""}, "float64": {"float64", ""}, "duration": {"duration", ""},
		"count": {"count", ""}, "ip": {"ip", ""}, "ipnet": {"cidr", ""}, "ipmask": {"string", ""},
		"hex": {"hexbytes", ""}, "b64": {"base64bytes", ""},
		"stringslice": {"[]string", ","}, "stringarray": {"[]string", ""}, "intslice": {"[]int", ","},
		"int32slice": {"[]int32", ","}, "int64slice": {"[]int64", ","}, "uintslice": {"[]uint", ","},
		"float32slice": {"[]float32", ","}, "float64slice": {"[]float64", ","}, "boolslice": {"[]bool", ","},
		"durationslice": {"[]duration", ","}, "ipslice": {"[]ip", ","}, "nets": {"[]cidr", ","},
		"s2s": {"map[string]string", ","}, "s2i": {"map[string]int", ","}, "s2i64": {"map[string]int64", ","},
		"flavor": {"string", ""}, "when": {"time", ""}, "fn": {"string", ""}, "bfn": {"bool", ""},
	}
	for name, w := range want {
		s := flagNamed(t, res.Command, name).Schema
		if s.Type != w[0] || s.Separator != w[1] {
			t.Errorf("--%s: type %q separator %q, want %q %q", name, s.Type, s.Separator, w[0], w[1])
		}
		if s.Default != nil {
			t.Errorf("--%s: zero default %v kept", name, s.Default)
		}
	}
	wantNote(t, res, "app", imp.Unsupported, "--ipmask is an IP mask")
	wantNote(t, res, "app", imp.Lossy, `--flavor has the custom value type "flavor"`)
	wantNote(t, res, "app", imp.Lossy, "--when is a time flag")
	wantNote(t, res, "app", imp.Lossy, "--fn calls a function with its value")
	wantNote(t, res, "app", imp.Lossy, "--bfn calls a function when set")
}

func TestFlagDetails(t *testing.T) {
	root := &cobra.Command{Use: "app", Run: run}
	f := root.Flags()
	f.StringP("file", "f", "", "read `PATH` for input")
	_ = root.MarkFlagFilename("file", "yaml", "*.json")
	f.String("any-file", "", "")
	_ = root.MarkFlagFilename("any-file")
	f.String("dir", "", "")
	_ = root.MarkFlagDirname("dir")
	f.String("sub", "", "")
	_ = f.SetAnnotation("sub", cobra.BashCompSubdirsInDir, []string{"charts"})
	f.String("legacy", "", "")
	_ = f.SetAnnotation("legacy", cobra.BashCompCustom, []string{"__fn"})
	f.String("name", "", "")
	_ = root.MarkFlagRequired("name")
	f.String("region", "eu", "")
	_ = root.MarkFlagRequired("region")
	f.String("color", "auto", "")
	f.Lookup("color").NoOptDefVal = "always"
	f.Bool("odd", false, "")
	f.Lookup("odd").NoOptDefVal = "false"
	f.StringSlice("tags", []string{"a", "b,c"}, "")
	f.Lookup("tags").NoOptDefVal = "x"
	f.StringToString("labels", map[string]string{"k": "v"}, "")
	f.Int("n", 7, "")
	f.Bool("wait", true, "")
	f.String("old", "", "")
	_ = f.MarkDeprecated("old", "use --name")
	f.String("secret", "", "")
	_ = f.MarkHidden("secret")
	f.StringP("namespace", "n", "", "")
	_ = f.MarkShorthandDeprecated("namespace", "spell it out")
	f.StringP("one", "1", "", "")
	f.String("dotted.name", "", "")
	res := importTree(t, root, cobraimport.Options{})
	c := res.Command

	file := flagNamed(t, c, "file")
	if !reflect.DeepEqual(file.Identifiers, []string{"-f", "--file"}) || file.Summary != "read PATH for input" ||
		file.Schema.Placeholder != "PATH" || file.Schema.Complete.Kind != "file" ||
		!reflect.DeepEqual(file.Schema.Complete.Extensions, []string{"yaml", "json"}) {
		t.Fatalf("file %+v %+v", file, file.Schema.Complete)
	}
	if cc := flagNamed(t, c, "any-file").Schema.Complete; cc.Kind != "file" || cc.Extensions != nil {
		t.Fatalf("any-file %+v", cc)
	}
	if cc := flagNamed(t, c, "dir").Schema.Complete; cc.Kind != "directory" {
		t.Fatalf("dir %+v", cc)
	}
	wantNote(t, res, "app", imp.Lossy, "--sub completes the sub-directories of [charts]")
	wantNote(t, res, "app", imp.Unsupported, "--legacy uses a legacy bash completion function")
	if !flagNamed(t, c, "name").Schema.Required {
		t.Fatal("--name not required")
	}
	wantNote(t, res, "app", imp.Lossy, "--region is required and has the default eu")
	if s := flagNamed(t, c, "color").Schema; s.ImplicitValue != "always" || s.Default != "auto" {
		t.Fatalf("color %+v", s)
	}
	wantNote(t, res, "app", imp.Unsupported, `--odd is a bool whose bare form means "false"`)
	wantNote(t, res, "app", imp.Unsupported, "--tags has a value for its bare form")
	if d := flagNamed(t, c, "tags").Schema.Default; !reflect.DeepEqual(d, []any{"a", "b,c"}) {
		t.Fatalf("tags default %#v", d)
	}
	if d := flagNamed(t, c, "labels").Schema.Default; !reflect.DeepEqual(d, map[string]any{"k": "v"}) {
		t.Fatalf("labels default %#v", d)
	}
	if d := flagNamed(t, c, "n").Schema.Default; d != int64(7) {
		t.Fatalf("n default %#v", d)
	}
	if d := flagNamed(t, c, "wait").Schema.Default; d != true {
		t.Fatalf("wait default %#v", d)
	}
	if old := flagNamed(t, c, "old"); old.Deprecated != "use --name" || !old.Hidden {
		t.Fatalf("old %+v", old)
	}
	if !flagNamed(t, c, "secret").Hidden {
		t.Fatal("secret not hidden")
	}
	ns := flagNamed(t, c, "namespace")
	if ns.Deprecated != "" || !reflect.DeepEqual(ns.DeprecatedIdentifiers, []string{"-n"}) {
		t.Fatalf("namespace %+v", ns)
	}
	wantNote(t, res, "app", imp.Lossy, `-n is deprecated; its message "spell it out"`)
	if ids := flagNamed(t, c, "one").Identifiers; !reflect.DeepEqual(ids, []string{"--one"}) {
		t.Fatalf("one %v", ids)
	}
	wantNote(t, res, "app", imp.Lossy, "-1 dropped from --one")
	if ids := flagNamed(t, c, "dotted-name").Identifiers; !reflect.DeepEqual(ids, []string{"--dotted-name"}) {
		t.Fatalf("dotted %v", ids)
	}
	wantNote(t, res, "app", imp.Lossy, `flag --dotted.name is not a valid rotini name; renamed to --dotted-name`)
	wantNote(t, res, "app", imp.Info, "deprecation warnings")
}

func TestPersistentAndLocalFlags(t *testing.T) {
	root := &cobra.Command{Use: "app"}
	root.PersistentFlags().String("region", "", "")
	root.Flags().Bool("local", false, "")
	child := &cobra.Command{Use: "child", Run: run}
	child.Flags().Int("n", 0, "")
	root.AddCommand(child)
	res := importTree(t, root, cobraimport.Options{})
	if !flagNamed(t, res.Command, "region").Cascading || flagNamed(t, res.Command, "local").Cascading {
		t.Fatal("cascading")
	}
	ch := find(t, res, "app child")
	if hasFlag(ch, "region") || !hasFlag(ch, "n") {
		t.Fatalf("child flags %+v", ch.Flags)
	}
	wantNote(t, res, "app", imp.Info, "own flags after a sub-command name (app)")
}

func groupsOf(c *imp.Command) []string {
	var out []string
	for _, g := range c.FlagGroups {
		out = append(out, g.Kind+":"+strings.Join(g.Flags, ","))
	}
	return out
}

func TestFlagGroups(t *testing.T) {
	root := &cobra.Command{Use: "app"}
	root.PersistentFlags().String("user", "", "")
	root.PersistentFlags().String("password", "", "")
	root.MarkFlagsRequiredTogether("user", "password")
	child := &cobra.Command{Use: "child", Run: run}
	f := child.Flags()
	for _, n := range []string{"json", "yaml", "last", "to", "a", "b", "token"} {
		f.Bool(n, false, "")
	}
	child.MarkFlagsMutuallyExclusive("json", "yaml")
	child.MarkFlagsMutuallyExclusive("last", "to")
	_ = f.SetAnnotation("last", "cobra_annotation_one_required", []string{"last to"})
	_ = f.SetAnnotation("to", "cobra_annotation_one_required", []string{"last to"})
	_ = f.SetAnnotation("a", "cobra_annotation_one_required", []string{"a b"})
	_ = f.SetAnnotation("b", "cobra_annotation_one_required", []string{"a b"})
	grand := &cobra.Command{Use: "grand", Run: run}
	child.AddCommand(grand)
	root.AddCommand(child)
	child.MarkFlagsMutuallyExclusive("token", "user")
	res := importTree(t, root, cobraimport.Options{})

	if got := groupsOf(res.Command); !reflect.DeepEqual(got, []string{"required_together:password,user"}) {
		t.Fatalf("root groups %v", got)
	}
	want := []string{"at_least_one:a,b", "mutually_exclusive:json,yaml", "one_of:last,to"}
	if got := groupsOf(find(t, res, "app child")); !reflect.DeepEqual(got, want) {
		t.Fatalf("child groups %v, want %v", got, want)
	}
	if got := groupsOf(find(t, res, "app child grand")); got != nil {
		t.Fatalf("grand groups %v", got)
	}
	wantNote(t, res, "app child", imp.Unsupported, "mutually_exclusive group [token user] mixes")
	noNote(t, res, "app", "mixes")
	noNote(t, res, "app child grand", "mixes")
}

func TestArgsValidators(t *testing.T) {
	cases := []struct {
		name string
		cmd  *cobra.Command
		want string
		note string
	}{
		{"none", &cobra.Command{Use: "x", Args: cobra.NoArgs}, "", ""},
		{"exact named", &cobra.Command{Use: "x <src> <dst>", Args: cobra.ExactArgs(2)}, "src:string! dst:string!", ""},
		{"exact unnamed", &cobra.Command{Use: "x", Args: cobra.ExactArgs(2)}, "arg1:string! arg2:string!", "argument names invented"},
		{"range", &cobra.Command{Use: "x <svc> [rev]", Args: cobra.RangeArgs(1, 2)}, "svc:string! rev:string", ""},
		{"minimum variadic", &cobra.Command{Use: "x NAME...", Args: cobra.MinimumNArgs(1)}, "name:[]string!>=1", ""},
		{"minimum named", &cobra.Command{Use: "x <a> <b> [rest...]", Args: cobra.MinimumNArgs(2)}, "a:string! b:string! rest:[]string", ""},
		{"maximum", &cobra.Command{Use: "x [a] [b]", Args: cobra.MaximumNArgs(2)}, "a:string b:string", ""},
		{"arbitrary", &cobra.Command{Use: "x", Args: cobra.ArbitraryArgs}, "args:[]string", "argument names invented"},
		{"only valid", &cobra.Command{Use: "x <kind>", Args: cobra.OnlyValidArgs, ValidArgs: []string{"a\tthe a", "b"}}, "kind:[]string", ""},
		{"match all", &cobra.Command{Use: "x <kind>", Args: cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs), ValidArgs: []string{"a", "b"}}, "kind:string!", ""},
		{"custom range", &cobra.Command{Use: "x [name]", Args: func(_ *cobra.Command, a []string) error {
			if len(a) > 1 {
				return errors.New("too many")
			}
			return nil
		}}, "name:string", "custom Args validator"},
		{"custom gaps", &cobra.Command{Use: "x", Args: func(_ *cobra.Command, a []string) error {
			if len(a)%2 == 1 {
				return errors.New("pairs")
			}
			return nil
		}}, "args:[]string", "not a range"},
		{"custom never", &cobra.Command{Use: "x", Args: func(*cobra.Command, []string) error { return errors.New("no") }}, "args:[]string", "accepted none"},
		{"custom panics", &cobra.Command{Use: "x", Args: func(_ *cobra.Command, a []string) error {
			if len(a) > 0 {
				panic(a[0])
			}
			return nil
		}}, "", "custom Args validator"},
		{"duplicate names", &cobra.Command{Use: "x NAME NAME", Args: cobra.ExactArgs(2)}, "name:string! name_2:string!", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := &cobra.Command{Use: "app"}
			tc.cmd.Run = run
			root.AddCommand(tc.cmd)
			res := importTree(t, root, cobraimport.Options{})
			c := find(t, res, "app x")
			if got := argSummary(c); got != tc.want {
				t.Fatalf("arguments %q, want %q\n%s", got, tc.want, dumpNotes(res))
			}
			if tc.note != "" && len(notesFor(res, "app x", tc.note)) != 1 {
				t.Fatalf("want a note containing %q:\n%s", tc.note, dumpNotes(res))
			}
		})
	}
}

func TestValidArgsEnum(t *testing.T) {
	root := &cobra.Command{Use: "app"}
	one := &cobra.Command{Use: "one <kind>", Args: cobra.ExactArgs(1), ValidArgs: []string{"a\tthe a", "b"}, Run: run}
	two := &cobra.Command{Use: "two <kind> <name>", Args: cobra.ExactArgs(2), ValidArgs: []string{"a"}, Run: run}
	many := &cobra.Command{Use: "many", Args: cobra.ArbitraryArgs, ValidArgs: []string{"a"}, Run: run}
	root.AddCommand(one, two, many)
	res := importTree(t, root, cobraimport.Options{})
	want := []imp.EnumValue{{Value: "a", Summary: "the a"}, {Value: "b"}}
	if got := find(t, res, "app one").Arguments[0].Schema.Enum; !reflect.DeepEqual(got, want) {
		t.Fatalf("enum %+v", got)
	}
	wantNote(t, res, "app two", imp.Lossy, "first argument only")
	if items := find(t, res, "app many").Arguments[0].Schema.Items; items == nil || len(items.Enum) != 1 {
		t.Fatalf("variadic enum %+v", items)
	}
}

func TestArgsUnset(t *testing.T) {
	build := func() *cobra.Command {
		root := &cobra.Command{Use: "app"}
		root.AddCommand(
			&cobra.Command{Use: "get <key> [default]", Run: run},
			&cobra.Command{Use: "rm <name>...", Run: run},
			&cobra.Command{Use: "mv [src] <dst>", Run: run},
			&cobra.Command{Use: "topic", Short: "about", Long: "Details."},
			&cobra.Command{Use: "pick", ValidArgs: []string{"a"}, Run: run},
		)
		return root
	}
	res := importTree(t, build(), cobraimport.Options{})
	cases := map[string]string{
		"app get":   "key:string! default:string args:[]string",
		"app rm":    "name:[]string!>=1",
		"app mv":    "src:string dst:string args:[]string",
		"app topic": "",
		"app pick":  "args:[]string",
	}
	for path, want := range cases {
		if got := argSummary(find(t, res, path)); got != want {
			t.Errorf("%s: %q, want %q", path, got, want)
		}
	}
	if len(res.Command.Arguments) != 0 {
		t.Fatal("a root with sub-commands took arguments")
	}
	wantNote(t, res, "app get", imp.Info, "Args unset")
	wantNote(t, res, "app mv", imp.Lossy, "<dst> is required after an optional placeholder")
	wantNote(t, res, "app topic", imp.Info, "help topic")
	noNote(t, res, "app rm", "Args unset")

	strict := importTree(t, build(), cobraimport.Options{Strict: true})
	if got := argSummary(find(t, strict, "app get")); got != "key:string! default:string" {
		t.Fatalf("strict get %q", got)
	}
	noNote(t, strict, "app get", "Args unset")
}

func TestPassthrough(t *testing.T) {
	root := &cobra.Command{Use: "app"}
	exec := &cobra.Command{Use: "exec -- CMD", DisableFlagParsing: true, Run: run}
	wrap := &cobra.Command{Use: "wrap", DisableFlagParsing: true, Run: run}
	wrap.Flags().Bool("x", false, "")
	root.AddCommand(exec, wrap)
	res := importTree(t, root, cobraimport.Options{})
	c := find(t, res, "app exec")
	if !c.Passthrough || argSummary(c) != "args:[]string" || c.Flags != nil {
		t.Fatalf("exec passthrough=%v %s %+v", c.Passthrough, argSummary(c), c.Flags)
	}
	noNote(t, res, "app exec", "DisableFlagParsing")
	noNote(t, res, "app exec", "were dropped")
	wantNote(t, res, "app wrap", imp.Unsupported, "were dropped")
}

func TestCommandFields(t *testing.T) {
	root := &cobra.Command{Use: "app", Short: "An app", Long: "An app", Aliases: []string{"a"}}
	root.Annotations = map[string]string{"cobra_annotation_command_display_name": "kubectl app"}
	root.AddGroup(&cobra.Group{ID: "core", Title: "Core Commands:"})
	deploy := &cobra.Command{
		Use: "deploy", Short: " Deploy ", Long: "Deploys.\nReally.", GroupID: "core",
		Aliases: []string{"d", "bad.alias"}, Hidden: true, Deprecated: "use ship",
		Example: "  app deploy\n\n  app deploy --x\n", SuggestFor: []string{"dep"},
		TraverseChildren: true, Run: run,
	}
	deploy.FParseErrWhitelist.UnknownFlags = true
	odd := &cobra.Command{Use: "config:set", Run: run}
	root.AddCommand(deploy, odd)
	res := importTree(t, root, cobraimport.Options{})
	if r := res.Command; r.Description != "" || r.DisplayName != "kubectl app" || r.Aliases != nil {
		t.Fatalf("root %+v", r)
	}
	wantNote(t, res, "app", imp.Info, "aliases [a] on the root dropped")
	d := find(t, res, "app deploy")
	if d.Summary != "Deploy" || d.Description != "Deploys.\nReally." || d.Group != "Core Commands" ||
		!reflect.DeepEqual(d.Aliases, []string{"d"}) || !d.Hidden || d.Deprecated != "use ship" ||
		!reflect.DeepEqual(d.Examples, []string{"app deploy", "app deploy --x"}) {
		t.Fatalf("deploy %+v", d)
	}
	wantNote(t, res, "app deploy", imp.Lossy, `alias "bad.alias" dropped`)
	wantNote(t, res, "app deploy", imp.Unsupported, "SuggestFor [dep]")
	wantNote(t, res, "app deploy", imp.Info, "TraverseChildren")
	wantNote(t, res, "app deploy", imp.Unsupported, "FParseErrWhitelist")
	find(t, res, "app config-set")
	wantNote(t, res, "app config-set", imp.Lossy, `command name "config:set" is not a valid rotini name`)
	wantNote(t, res, "app", imp.Info, "deprecation warnings")
	wantNote(t, res, "app", imp.Info, "alphabetical order")
}

func TestHooks(t *testing.T) {
	root := &cobra.Command{Use: "app", PersistentPreRunE: func(*cobra.Command, []string) error { return nil }}
	group := &cobra.Command{Use: "group", PersistentPostRun: run}
	leaf := &cobra.Command{Use: "leaf", PreRun: run, RunE: func(*cobra.Command, []string) error { return nil }, PostRunE: func(*cobra.Command, []string) error { return nil }}
	group.AddCommand(leaf)
	root.AddCommand(group)
	res := importTree(t, root, cobraimport.Options{})
	var got []string
	for _, h := range res.Hooks {
		if h.File == "" || h.Line == 0 || !strings.HasSuffix(h.File, "cobra_test.go") {
			t.Fatalf("hook position %+v", h)
		}
		got = append(got, h.Path+" "+h.Framework+"->"+h.Rotini)
	}
	want := []string{
		"app PersistentPreRunE->CascadingPreRun",
		"app group PersistentPostRun->CascadingPostRun",
		"app group leaf PreRun->PreRun",
		"app group leaf RunE->Run",
		"app group leaf PostRunE->PostRun",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hooks %v", got)
	}
	wantNote(t, res, "app", imp.Info, "nearest PersistentPreRun")
	wantNote(t, res, "app", imp.Info, "not in the walked tree")
	wantNote(t, res, "app", imp.Info, "help for a command with sub-commands and no Run")
}

func TestTemplates(t *testing.T) {
	root := &cobra.Command{Use: "app", Version: "1"}
	root.SetHelpFunc(func(*cobra.Command, []string) {})
	root.SetUsageTemplate("usage")
	root.SetVersionTemplate("v")
	child := &cobra.Command{Use: "child", Run: run}
	child.SetUsageFunc(func(*cobra.Command) error { return nil })
	child.SetHelpTemplate("help")
	root.AddCommand(child)
	res := importTree(t, root, cobraimport.Options{})
	wantNote(t, res, "app", imp.Lossy, "custom help function")
	wantNote(t, res, "app", imp.Lossy, "custom usage template")
	wantNote(t, res, "app", imp.Lossy, "custom version template")
	noNote(t, res, "app child", "custom help function")
	noNote(t, res, "app child", "custom usage template")
	wantNote(t, res, "app child", imp.Lossy, "custom usage function")
	wantNote(t, res, "app child", imp.Lossy, "custom help template")
}

func TestCompletionFunctions(t *testing.T) {
	root := &cobra.Command{Use: "app"}
	none := &cobra.Command{Use: "none <a>", Args: cobra.ExactArgs(1), ValidArgsFunction: cobra.NoFileCompletions, Run: run}
	fixed := &cobra.Command{Use: "fixed <a>", Args: cobra.ExactArgs(1), ValidArgsFunction: cobra.FixedCompletions([]string{"x\tdesc", "y"}, cobra.ShellCompDirectiveNoFileComp), Run: run}
	fn := &cobra.Command{Use: "fn <a>", Args: cobra.ExactArgs(1), Run: run,
		ValidArgsFunction: func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			return nil, cobra.ShellCompDirectiveDefault
		}}
	noargs := &cobra.Command{Use: "noargs", Args: cobra.NoArgs, ValidArgsFunction: cobra.NoFileCompletions, Run: run}
	flagged := &cobra.Command{Use: "flagged", Args: cobra.NoArgs, Run: run}
	flagged.Flags().String("kind", "", "")
	_ = flagged.RegisterFlagCompletionFunc("kind", cobra.NoFileCompletions)
	root.AddCommand(none, fixed, fn, noargs, flagged)
	res := importTree(t, root, cobraimport.Options{})
	if cc := find(t, res, "app none").Arguments[0].Schema.Complete; cc == nil || cc.Kind != "none" {
		t.Fatalf("none %+v", cc)
	}
	wantNote(t, res, "app fixed", imp.Lossy, "completes the fixed list [x y]")
	wantNote(t, res, "app fn", imp.Lossy, "has a Go completion function")
	wantNote(t, res, "app noargs", imp.Info, "takes no arguments")

	// Flag completion functions are visible from Cobra 1.8 on.
	cc := flagNamed(t, find(t, res, "app flagged"), "kind").Schema.Complete
	if modernCobra() {
		if cc == nil || cc.Kind != "none" {
			t.Fatalf("flag completion %+v", cc)
		}
		noNote(t, res, "app", "does not expose flag completion")
	} else {
		if cc != nil {
			t.Fatalf("flag completion %+v", cc)
		}
		wantNote(t, res, "app", imp.Info, "does not expose flag completion")
	}
}

func TestGoFlagSet(t *testing.T) {
	root := &cobra.Command{Use: "app", Run: run}
	fs := pflag.NewFlagSet("x", pflag.ContinueOnError)
	fs.Duration("wait", time.Second, "")
	root.Flags().AddFlagSet(fs)
	res := importTree(t, root, cobraimport.Options{})
	if s := flagNamed(t, res.Command, "wait").Schema; s.Type != "duration" || s.Default != "1s" {
		t.Fatalf("wait %+v", s)
	}
}
