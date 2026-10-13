package urfave_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	imp "github.com/go-rotini/import"
	urfaveimport "github.com/go-rotini/import/urfave"
	"github.com/urfave/cli/v3"
)

func action(context.Context, *cli.Command) error { return nil }

func importTree(t *testing.T, root *cli.Command, opts urfaveimport.Options) *imp.Result {
	t.Helper()
	res, err := urfaveimport.FromURFave(root, opts)
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

func wantNote(t *testing.T, res *imp.Result, path string, level imp.Level, substr string) {
	t.Helper()
	var got []imp.Note
	for _, n := range res.Notes {
		if n.Path == path && strings.Contains(n.Msg, substr) {
			got = append(got, n)
		}
	}
	if len(got) != 1 {
		t.Fatalf("want one note on %q containing %q, got %d:\n%s", path, substr, len(got), dumpNotes(res))
	}
	if got[0].Level != level {
		t.Fatalf("note %q is %s, want %s", got[0].Msg, got[0].Level, level)
	}
}

func dumpNotes(res *imp.Result) string {
	var b strings.Builder
	for _, n := range res.Notes {
		fmt.Fprintln(&b, n)
	}
	return b.String()
}

func names(cs []*imp.Command) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return out
}

// setField sets a field newer urfave/cli releases have, reporting whether this one has it.
func setField(p any, name string, v any) bool {
	f := reflect.ValueOf(p).Elem().FieldByName(name)
	if !f.IsValid() {
		return false
	}
	f.Set(reflect.ValueOf(v))
	return true
}

// multiArg returns the argument type that takes several values, in this urfave/cli release.
func multiArg(name string, lo, hi int) cli.Argument {
	v := reflect.New(reflect.TypeOf(cli.AnyArguments[0]).Elem())
	v.Elem().FieldByName("Name").SetString(name)
	v.Elem().FieldByName("Min").SetInt(int64(lo))
	v.Elem().FieldByName("Max").SetInt(int64(hi))
	a, _ := v.Interface().(cli.Argument)
	return a
}

func TestFromURFaveErrors(t *testing.T) {
	if _, err := urfaveimport.FromURFave(nil, urfaveimport.Options{}); err == nil {
		t.Fatal("want an error")
	}
	root := &cli.Command{Name: "app", Flags: []cli.Flag{plainFlag{}}}
	if _, err := urfaveimport.FromURFave(root, urfaveimport.Options{}); err == nil {
		t.Fatal("want the panic as an error")
	}
}

func TestRootName(t *testing.T) {
	res := importTree(t, &cli.Command{}, urfaveimport.Options{})
	want := strings.TrimSuffix(strings.TrimSuffix(lastElem(os.Args[0]), ".exe"), ".test")
	if res.Command.Name != want {
		t.Fatalf("root %q, want %q", res.Command.Name, want)
	}
	if res := importTree(t, &cli.Command{}, urfaveimport.Options{Name: "tool"}); res.Command.Name != "tool" {
		t.Fatalf("root %q", res.Command.Name)
	}
	res = importTree(t, &cli.Command{Name: "my tool"}, urfaveimport.Options{Name: "ignored"})
	if res.Command.Name != "my-tool" || res.Source.Framework != "urfave" {
		t.Fatalf("root %q, framework %q", res.Command.Name, res.Source.Framework)
	}
	wantNote(t, res, "my-tool", imp.Lossy, `command name "my tool" is not a valid rotini name`)
}

func lastElem(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

func TestCommands(t *testing.T) {
	root := &cli.Command{
		Name:        "app",
		Aliases:     []string{"a"},
		Usage:       "An app",
		Description: "An app",
		Before:      func(ctx context.Context, _ *cli.Command) (context.Context, error) { return ctx, nil },
		After:       action,
		Commands: []*cli.Command{
			{Name: "deploy", Aliases: []string{"d", "bad alias"}, Usage: "Deploy", Description: "Deploys things.", Category: "Core", Hidden: true, Action: action},
			{Name: "group", Commands: []*cli.Command{{Name: "leaf", Action: action}}},
		},
	}
	res := importTree(t, root, urfaveimport.Options{})
	if res.Command.Description != "" || res.Command.Aliases != nil {
		t.Fatalf("root = %+v", res.Command)
	}
	wantNote(t, res, "app", imp.Info, "aliases [a] on the root dropped")
	d := find(t, res, "app deploy")
	if d.Summary != "Deploy" || d.Description != "Deploys things." || d.Group != "Core" || !d.Hidden || !reflect.DeepEqual(d.Aliases, []string{"d"}) {
		t.Fatalf("deploy = %+v", d)
	}
	wantNote(t, res, "app deploy", imp.Lossy, `alias "bad alias" dropped`)
	if got := names(res.Command.Commands); !reflect.DeepEqual(got, []string{"deploy", "group", "help"}) {
		t.Fatalf("commands %v", got)
	}
	help := find(t, res, "app help")
	if help.Builtin != "help" || len(help.Arguments) != 1 || help.Arguments[0].Schema.Type != "[]string" {
		t.Fatalf("help = %+v", help)
	}
	var hooks []string
	for _, h := range res.Hooks {
		hooks = append(hooks, h.Path+" "+h.Framework+"->"+h.Rotini)
		if h.File == "" || h.Line == 0 {
			t.Errorf("hook %+v has no position", h)
		}
	}
	want := []string{"app Before->CascadingPreRun", "app After->CascadingPostRun", "app deploy Action->Run", "app group leaf Action->Run"}
	if !reflect.DeepEqual(hooks, want) {
		t.Fatalf("hooks %v, want %v", hooks, want)
	}
	wantNote(t, res, "app", imp.Info, "urfave/cli prints help for a command with no Action")
}

func TestBuiltins(t *testing.T) {
	root := &cli.Command{
		Name:                  "app",
		Version:               "1.2.3",
		EnableShellCompletion: true,
		Flags:                 []cli.Flag{&cli.BoolFlag{Name: "verbose", Aliases: []string{"v"}}},
		Commands:              []*cli.Command{{Name: "run", Action: action}},
	}
	res := importTree(t, root, urfaveimport.Options{})
	flags := res.Command.Flags
	if h := flags[0]; h.Builtin != "help" || !h.Cascading || !h.ShortCircuit || !reflect.DeepEqual(h.Identifiers, []string{"-h", "--help"}) {
		t.Fatalf("help flag %+v", h)
	}
	v := flags[len(flags)-1]
	if v.Builtin != "version" || v.Cascading || !reflect.DeepEqual(v.Identifiers, []string{"--version"}) {
		t.Fatalf("version flag %+v (the root's -v must win)", v)
	}
	wantNote(t, res, "app", imp.Info, `Version "1.2.3"`)
	if got := names(res.Command.Commands); !reflect.DeepEqual(got, []string{"run", "help", "completion"}) {
		t.Fatalf("commands %v", got)
	}
	c := find(t, res, "app completion")
	if c.Builtin != "completion" || !c.Hidden || len(c.Arguments[0].Schema.Enum) != 4 || !c.Arguments[0].Schema.Required {
		t.Fatalf("completion = %+v", c)
	}
	if got := c.Arguments[0].Schema.Enum[3].Value; got != "powershell" {
		t.Fatalf("completion shell %q", got)
	}
	wantNote(t, res, "app completion", imp.Lossy, "where urfave/cli's pwsh is powershell")

	hidden := &cli.Command{Name: "app", Version: "1", HideVersion: true, HideHelpCommand: true, ShellCompletionCommandName: "complete"}
	res = importTree(t, hidden, urfaveimport.Options{})
	if len(res.Command.Flags) != 1 || res.Command.Flags[0].Builtin != "help" || len(res.Command.Commands) != 0 {
		t.Fatalf("hidden builtins %+v %v", res.Command.Flags, names(res.Command.Commands))
	}
	none := &cli.Command{Name: "app", HideHelp: true, EnableShellCompletion: true, ShellCompletionCommandName: "complete",
		Flags: []cli.Flag{&cli.StringFlag{Name: "help"}}}
	res = importTree(t, none, urfaveimport.Options{})
	if got := names(res.Command.Commands); !reflect.DeepEqual(got, []string{"complete"}) {
		t.Fatalf("commands %v", got)
	}
	if f := res.Command.Flags; len(f) != 1 || f[0].Builtin != "" {
		t.Fatalf("flags %+v", f)
	}
	taken := &cli.Command{Name: "app", Flags: []cli.Flag{&cli.StringFlag{Name: "help"}}}
	if res := importTree(t, taken, urfaveimport.Options{}); len(res.Command.Flags) != 1 || res.Command.Flags[0].Builtin != "" {
		t.Fatalf("a root flag named help must keep the builtin out: %+v", res.Command.Flags)
	}
}

type custom struct{ v string }

func (c *custom) Set(s string) error { c.v = s; return nil }
func (c *custom) String() string     { return c.v }
func (c *custom) Get() any           { return c.v }

type badFlag struct{ cli.BoolFlag }

func (badFlag) Names() []string { return nil }

type plainFlag struct{ cli.Flag }

func TestFlagTypes(t *testing.T) {
	count := 0
	root := &cli.Command{
		Name: "app",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "v", Config: cli.BoolConfig{Count: &count}},
			&cli.BoolFlag{Name: "dry-run", Value: true},
			&cli.StringFlag{Name: "name", Aliases: []string{"n", "full-name"}, Value: "x", Usage: "your `NAME`"},
			&cli.IntFlag{Name: "replicas", Value: 3},
			&cli.UintFlag{Name: "nodes"},
			&cli.FloatFlag{Name: "weight", Value: 0.5},
			&cli.DurationFlag{Name: "timeout", Value: time.Minute},
			&cli.TimestampFlag{Name: "at", Config: cli.TimestampConfig{Layouts: []string{"2006-01-02"}, Timezone: time.UTC}},
			&cli.StringSliceFlag{Name: "tags", Value: []string{"a", "b"}},
			&cli.StringMapFlag{Name: "set", Value: map[string]string{"a": "1"}},
			&cli.GenericFlag{Name: "format", Value: &custom{"json"}},
			&cli.IntFlag{Name: "mode", Config: cli.IntegerConfig{Base: 8}},
			&badFlag{},
			&plainFlag{&cli.BoolFlag{Name: "plain"}},
		},
	}
	res := importTree(t, root, urfaveimport.Options{})
	intType := reflect.TypeOf(cli.IntFlag{}.Value).Kind().String()
	uintType := reflect.TypeOf(cli.UintFlag{}.Value).Kind().String()
	floatType := reflect.TypeOf(cli.FloatFlag{}.Value).Kind().String()
	cases := []struct {
		flag, typ string
		def       any
		sep       string
	}{
		{"v", "count", nil, ""},
		{"dry-run", "bool", true, ""},
		{"name", "string", "x", ""},
		{"replicas", intType, int64(3), ""},
		{"nodes", uintType, nil, ""},
		{"weight", floatType, 0.5, ""},
		{"timeout", "duration", "1m0s", ""},
		{"at", "time", nil, ""},
		{"tags", "[]string", []any{"a", "b"}, ","},
		{"set", "map[string]string", map[string]any{"a": "1"}, ","},
		{"format", "string", "json", ""},
		{"mode", intType, nil, ""},
		{"plain", "string", nil, ""},
	}
	for _, tc := range cases {
		f := flagNamed(t, res.Command, tc.flag)
		if f.Schema.Type != tc.typ || !reflect.DeepEqual(f.Schema.Default, tc.def) || f.Schema.Separator != tc.sep {
			t.Errorf("--%s: type %q default %#v separator %q; want %q %#v %q", tc.flag, f.Schema.Type, f.Schema.Default, f.Schema.Separator, tc.typ, tc.def, tc.sep)
		}
	}
	n := flagNamed(t, res.Command, "name")
	if !reflect.DeepEqual(n.Identifiers, []string{"-n", "--name", "--full-name"}) || n.Schema.Placeholder != "NAME" || n.Summary != "your NAME" {
		t.Errorf("--name = %+v %+v", n, n.Schema)
	}
	if v := flagNamed(t, res.Command, "v"); !reflect.DeepEqual(v.Identifiers, []string{"-v"}) {
		t.Errorf("-v identifiers %v", v.Identifiers)
	}
	wantNote(t, res, "app", imp.Lossy, "--format has the custom value type *urfave_test.custom")
	wantNote(t, res, "app", imp.Lossy, "--mode parses numbers in base 8")
	wantNote(t, res, "app", imp.Lossy, "--at parses times with the layouts [2006-01-02]")
	wantNote(t, res, "app", imp.Lossy, "--at reads times in the zone UTC")
	wantNote(t, res, "app", imp.Unsupported, "has no name, so it was dropped")
	wantNote(t, res, "app", imp.Lossy, "--plain is a flag of the custom type *urfave_test.plainFlag")
}

func TestFlagDetails(t *testing.T) {
	root := &cli.Command{
		Name: "app",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "token", Hidden: true, Required: true, Value: "x", Category: "Auth",
				Sources: cli.NewValueSourceChain(cli.EnvVar("APP_TOKEN"), cli.EnvVar("bad-env"), cli.File("/run/token"))},
			&cli.StringFlag{Name: "config", TakesFile: true, OnlyOnce: true,
				Action:    func(context.Context, *cli.Command, string) error { return nil },
				Validator: func(string) error { return nil }},
			&cli.StringSliceFlag{Name: "tags", OnlyOnce: true},
			&cli.BoolWithInverseFlag{Name: "wait", Aliases: []string{"w"}},
			&cli.BoolWithInverseFlag{Name: "color", Aliases: []string{"colour"}, InversePrefix: "plain-"},
			&cli.StringFlag{Name: "a.b"},
			&cli.StringFlag{Name: "local", Local: true},
		},
		Commands: []*cli.Command{{Name: "sub", Action: action}},
	}
	setField(root.Flags[0], "Deprecated", "use --key")
	res := importTree(t, root, urfaveimport.Options{})
	tok := flagNamed(t, res.Command, "token")
	if !tok.Hidden || !tok.Schema.Required || tok.Schema.Default != nil || tok.Group != "Auth" || !tok.Cascading {
		t.Fatalf("--token = %+v %+v", tok, tok.Schema)
	}
	if !reflect.DeepEqual(tok.Schema.Variable, []string{"APP_TOKEN"}) {
		t.Fatalf("--token variables %v", tok.Schema.Variable)
	}
	wantNote(t, res, "app", imp.Lossy, "--token is required and has the default x")
	wantNote(t, res, "app", imp.Lossy, `environment variable "bad-env"`)
	wantNote(t, res, "app", imp.Lossy, `--token also reads file "/run/token"`)
	cfg := flagNamed(t, res.Command, "config")
	if cfg.Schema.Complete == nil || cfg.Schema.Complete.Kind != "file" || cfg.Schema.Repeatable == nil || *cfg.Schema.Repeatable {
		t.Fatalf("--config = %+v", cfg.Schema)
	}
	wantNote(t, res, "app", imp.Lossy, "--config runs")
	wantNote(t, res, "app", imp.Lossy, "--config checks its value with")
	wantNote(t, res, "app", imp.Lossy, "--tags may be given only once")
	if w := flagNamed(t, res.Command, "wait"); w.Schema.Negatable != true || !reflect.DeepEqual(w.Identifiers, []string{"-w", "--wait"}) {
		t.Fatalf("--wait = %+v %+v", w, w.Schema)
	}
	if c := flagNamed(t, res.Command, "color"); c.Schema.Negatable != "--plain-color" {
		t.Fatalf("--color negatable %#v", c.Schema.Negatable)
	}
	wantNote(t, res, "app", imp.Lossy, "--color takes the negated forms [--plain-color --plain-colour]")
	if ab := flagNamed(t, res.Command, "a-b"); !reflect.DeepEqual(ab.Identifiers, []string{"--a-b"}) {
		t.Fatalf("--a-b identifiers %v", ab.Identifiers)
	}
	wantNote(t, res, "app", imp.Lossy, "flag --a.b is not a valid rotini name; renamed to --a-b")
	if l := flagNamed(t, res.Command, "local"); l.Cascading {
		t.Fatal("--local cascades")
	}
	wantNote(t, res, "app", imp.Info, "rotini also accepts a command's own flags after a sub-command name (app)")
	if tok.Deprecated != "" {
		wantNote(t, res, "app", imp.Info, "urfave/cli prints deprecation warnings itself")
	}
}

func TestSeparators(t *testing.T) {
	flags := func() []cli.Flag { return []cli.Flag{&cli.StringSliceFlag{Name: "tags"}} }
	for _, tc := range []struct {
		root *cli.Command
		want string
	}{
		{&cli.Command{Name: "app", Flags: flags(), SliceFlagSeparator: ";"}, ";"},
		{&cli.Command{Name: "app", Flags: flags(), DisableSliceFlagSeparator: true}, ""},
		{&cli.Command{Name: "app", Flags: flags(), SliceFlagSeparator: "::"}, ""},
	} {
		res := importTree(t, tc.root, urfaveimport.Options{})
		if got := flagNamed(t, res.Command, "tags").Schema.Separator; got != tc.want {
			t.Errorf("separator %q, want %q", got, tc.want)
		}
	}
	root := &cli.Command{Name: "app", Flags: flags(), SliceFlagSeparator: "::"}
	wantNote(t, importTree(t, root, urfaveimport.Options{}), "app", imp.Lossy, `SliceFlagSeparator "::" dropped`)
	if setField(root, "MapFlagKeyValueSeparator", ":") {
		root.Flags = []cli.Flag{&cli.StringMapFlag{Name: "set"}}
		wantNote(t, importTree(t, root, urfaveimport.Options{}), "app", imp.Lossy, `--set splits keys from values on ":"`)
	}
}

func TestFlagGroups(t *testing.T) {
	root := &cli.Command{
		Name: "app",
		MutuallyExclusiveFlags: []cli.MutuallyExclusiveFlags{
			{Category: "Output", Flags: [][]cli.Flag{{&cli.BoolFlag{Name: "json"}}, {&cli.BoolFlag{Name: "yaml"}}}},
			{Required: true, Flags: [][]cli.Flag{{&cli.BoolFlag{Name: "last"}}, {&cli.StringFlag{Name: "to"}}}},
			{Flags: [][]cli.Flag{{&cli.StringFlag{Name: "cert"}, &cli.StringFlag{Name: "key"}}, {&cli.BoolFlag{Name: "insecure"}}}},
		},
	}
	res := importTree(t, root, urfaveimport.Options{})
	var got []string
	for _, g := range res.Command.FlagGroups {
		got = append(got, g.Kind+" "+strings.Join(g.Flags, ","))
	}
	if want := []string{"mutually_exclusive json,yaml", "one_of last,to"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("groups %v, want %v", got, want)
	}
	if g := flagNamed(t, res.Command, "json").Group; g != "Output" {
		t.Fatalf("--json group %q", g)
	}
	flagNamed(t, res.Command, "insecure")
	wantNote(t, res, "app", imp.Unsupported, "an option of several flags")
}

func TestArguments(t *testing.T) {
	single := &cli.StringArg{Name: "service"}
	// Releases between the ones with Min and Max and the ones with Required have neither:
	// their single-value arguments are always optional.
	required := setField(single, "Required", true)
	if setField(single, "Min", 1) {
		required = true
	}
	setField(single, "Max", 1)
	withDefault := &cli.IntArg{Name: "revision", Value: 2}
	setField(withDefault, "Max", 1)
	root := &cli.Command{
		Name: "app",
		Commands: []*cli.Command{
			{Name: "deploy", Action: action, Arguments: []cli.Argument{single, withDefault, multiArg("rest", 1, -1)}},
			{Name: "bounded", Action: action, Arguments: []cli.Argument{multiArg("pair", 2, 2), multiArg("never", 0, 0)}},
			{Name: "usage", Action: action, ArgsUsage: "<src>... <dst>"},
			{Name: "order", Action: action, ArgsUsage: "[a] <b>"},
			{Name: "none", Action: action},
			{Name: "exec", Action: action, SkipFlagParsing: true, Flags: []cli.Flag{&cli.BoolFlag{Name: "x"}}},
		},
	}
	res := importTree(t, root, urfaveimport.Options{})
	summary := func(c *imp.Command) string {
		var parts []string
		for _, a := range c.Arguments {
			s := a.Name + ":" + a.Schema.Type
			if a.Schema.Required {
				s += "!"
			}
			if a.Schema.MinItems != nil {
				s += fmt.Sprintf(">=%d", *a.Schema.MinItems)
			}
			if a.Schema.MaxItems != nil {
				s += fmt.Sprintf("<=%d", *a.Schema.MaxItems)
			}
			if a.Schema.Default != nil {
				s += fmt.Sprintf("=%v", a.Schema.Default)
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, " ")
	}
	intType := reflect.TypeOf(cli.IntArg{}.Value).Kind().String()
	for path, want := range map[string]string{
		"app deploy":  "service:string" + map[bool]string{true: "!"}[required] + " revision:" + intType + "=2 rest:[]string!>=1",
		"app bounded": "pair:[]string!>=2<=2",
		"app usage":   "src:[]string!>=1",
		"app order":   "a:string b:string args:[]string",
		"app none":    "args:[]string",
		"app exec":    "args:[]string",
	} {
		if got := summary(find(t, res, path)); got != want {
			t.Errorf("%s arguments %q, want %q", path, got, want)
		}
	}
	wantNote(t, res, "app bounded", imp.Unsupported, "<never> takes at most 0 values")
	wantNote(t, res, "app order", imp.Lossy, "<b> is required after an optional placeholder")
	wantNote(t, res, "app none", imp.Info, "no Arguments declared")
	ex := find(t, res, "app exec")
	if !ex.Passthrough || len(ex.Flags) != 0 {
		t.Fatalf("exec = %+v", ex)
	}
	wantNote(t, res, "app exec", imp.Unsupported, "SkipFlagParsing with its own flags")

	strict := importTree(t, root, urfaveimport.Options{Strict: true})
	if got := summary(find(t, strict, "app none")); got != "" {
		t.Fatalf("strict none arguments %q", got)
	}
}

func TestStopOnNthArg(t *testing.T) {
	cmd := func(name string, n int, args ...cli.Argument) *cli.Command {
		c := &cli.Command{Name: name, Action: action, Arguments: args}
		if !setField(c, "StopOnNthArg", &n) {
			t.Skip("this urfave/cli has no StopOnNthArg")
		}
		return c
	}
	root := &cli.Command{Name: "app", Commands: []*cli.Command{
		cmd("zero", 0),
		cmd("one", 1, &cli.StringArg{Name: "node"}),
		cmd("rest", 1, &cli.StringArg{Name: "node"}, multiArg("rest", 0, -1)),
		cmd("tail", 2, &cli.StringArg{Name: "a"}, &cli.StringArg{Name: "b"}, multiArg("rest", 0, -1)),
		cmd("far", 5),
	}}
	res := importTree(t, root, urfaveimport.Options{})
	if !find(t, res, "app zero").Passthrough || !find(t, res, "app one").OptionsFirst || !find(t, res, "app rest").OptionsFirst {
		t.Fatal("StopOnNthArg 0 or 1 not mapped")
	}
	if a := find(t, res, "app tail").Arguments[2]; !a.Passthrough {
		t.Fatalf("tail's last argument = %+v", a)
	}
	wantNote(t, res, "app far", imp.Lossy, "StopOnNthArg 5 dropped")
}

func TestCommandNotes(t *testing.T) {
	root := &cli.Command{
		Name:                          "app",
		DefaultCommand:                "run",
		PrefixMatchCommands:           true,
		Suggest:                       true,
		ReadArgsFromStdin:             true,
		AllowExtFlags:                 true,
		UsageText:                     "app [options]",
		CustomRootCommandHelpTemplate: "{{.Name}}",
		ShellComplete:                 func(context.Context, *cli.Command) {},
		OnUsageError:                  func(context.Context, *cli.Command, error, bool) error { return nil },
		CommandNotFound:               func(context.Context, *cli.Command, string) {},
		InvalidFlagAccessHandler:      func(context.Context, *cli.Command, string) {},
		ExitErrHandler:                func(context.Context, *cli.Command, error) {},
		Commands:                      []*cli.Command{{Name: "run", Action: action, CustomHelpTemplate: "x"}},
	}
	res := importTree(t, root, urfaveimport.Options{})
	for _, n := range []struct {
		level imp.Level
		msg   string
	}{
		{imp.Unsupported, `DefaultCommand "run" dropped`},
		{imp.Unsupported, "PrefixMatchCommands dropped"},
		{imp.Unsupported, "Suggest dropped"},
		{imp.Unsupported, "ReadArgsFromStdin dropped"},
		{imp.Unsupported, "AllowExtFlags dropped"},
		{imp.Lossy, "custom UsageText"},
		{imp.Lossy, "custom help template"},
		{imp.Lossy, "ShellComplete"},
		{imp.Lossy, "OnUsageError"},
		{imp.Lossy, "CommandNotFound"},
		{imp.Lossy, "InvalidFlagAccessHandler"},
		{imp.Lossy, "ExitErrHandler"},
	} {
		wantNote(t, res, "app", n.level, n.msg)
	}
	wantNote(t, res, "app run", imp.Lossy, "custom help template")
}

// valFlag is a flag stored by value whose struct cannot be compared.
type valFlag struct {
	Name  string
	Extra []string
}

func (f valFlag) String() string                                { return f.Name }
func (f valFlag) Get() any                                      { return nil }
func (f valFlag) PreParse() error                               { return nil }
func (f valFlag) PostParse() error                              { return nil }
func (f valFlag) Set(string, string) error                      { return nil }
func (f valFlag) Names() []string                               { return []string{f.Name} }
func (f valFlag) IsSet() bool                                   { return false }
func (f valFlag) Apply(any) error                               { return nil }
func (f valFlag) TakesValue() bool                              { return true }
func (f valFlag) IsDefaultVisible() bool                        { return true }
func (f valFlag) GetDefaultText() string                        { return "" }
func (f valFlag) TypeName() string                              { return "x" }
func (f valFlag) GetValue() string                              { return "" }
func (f valFlag) IsRequired() bool                              { return false }
func (f valFlag) IsVisible() bool                               { return true }
func (f valFlag) GetUsage() string                              { return "" }
func (f valFlag) GetEnvVars() []string                          { return nil }
func (f valFlag) GetCategory() string                           { return "" }
func (f valFlag) IsLocal() bool                                 { return false }
func (f valFlag) IsBoolFlag() bool                              { return false }
func (f valFlag) Count() int                                    { return 0 }
func (f valFlag) IsMultiValueFlag() bool                        { return false }
func (f valFlag) PreParseOnly() bool                            { return false }
func (f valFlag) RunAction(context.Context, *cli.Command) error { return nil }

func TestUnhashableFlag(t *testing.T) {
	root := &cli.Command{Name: "app", Flags: []cli.Flag{valFlag{Name: "x", Extra: []string{"a"}}}}
	res := importTree(t, root, urfaveimport.Options{})
	flagNamed(t, res.Command, "x")
}

type actions struct{ fail bool }

func (a actions) run(context.Context, *cli.Command) error {
	if a.fail {
		return fmt.Errorf("failed")
	}
	return nil
}

func TestGroupThatRunsTakesArguments(t *testing.T) {
	o := actions{}
	build := func() *cli.Command {
		return &cli.Command{Name: "app", Commands: []*cli.Command{
			{Name: "grp", ArgsUsage: "[thing]", Action: o.run, Commands: []*cli.Command{{Name: "leaf", Action: action}}},
			{Name: "plain", Commands: []*cli.Command{{Name: "leaf", Action: action}}},
		}}
	}
	// urfave/cli runs the group's Action with words that name no sub-command.
	var got []string
	run := build()
	run.Commands[0].Action = func(_ context.Context, c *cli.Command) error { got = c.Args().Slice(); return nil }
	if err := run.Run(context.Background(), []string{"app", "grp", "extra"}); err != nil || !reflect.DeepEqual(got, []string{"extra"}) {
		t.Fatalf("urfave/cli ran grp with %v, %v", got, err)
	}

	res := importTree(t, build(), urfaveimport.Options{})
	var args []string
	for _, a := range find(t, res, "app grp").Arguments {
		args = append(args, a.Name+":"+a.Schema.Type)
	}
	if !reflect.DeepEqual(args, []string{"thing:string", "args:[]string"}) {
		t.Fatalf("grp arguments %v", args)
	}
	if len(find(t, res, "app plain").Arguments) != 0 {
		t.Fatal("a group with no Action took arguments")
	}
	wantNote(t, res, "app", imp.Info, `answers "<command> help"`)
	found := false
	for _, h := range res.Hooks {
		if h.Path == "app grp" {
			found = true
			if h.Func != "github.com/go-rotini/import/urfave_test.actions.run" || !strings.HasSuffix(h.File, "urfave_test.go") || h.Line == 0 {
				t.Fatalf("method value hook %+v", h)
			}
		}
	}
	if !found {
		t.Fatal("no hook for app grp")
	}
}

func TestFlagNamesThatCollideOnceValid(t *testing.T) {
	root := &cli.Command{Name: "app", Flags: []cli.Flag{
		&cli.StringFlag{Name: "foo.bar"},
		&cli.StringFlag{Name: "foo-bar"},
	}}
	res := importTree(t, root, urfaveimport.Options{})
	flagNamed(t, res.Command, "foo-bar")
	flagNamed(t, res.Command, "foo-bar-2")
	wantNote(t, res, "app", imp.Lossy, "which another already has")
}

func TestRenamedFlagDoesNotShadowInherited(t *testing.T) {
	root := &cli.Command{
		Name:     "app",
		Flags:    []cli.Flag{&cli.StringFlag{Name: "foo-bar"}},
		Commands: []*cli.Command{{Name: "sub", Flags: []cli.Flag{&cli.StringFlag{Name: "foo.bar"}}}},
	}
	res := importTree(t, root, urfaveimport.Options{})
	flagNamed(t, find(t, res, "app sub"), "foo-bar-2")
	wantNote(t, res, "app sub", imp.Lossy, "would be --foo-bar, the name of the inherited flag --foo-bar")
}
