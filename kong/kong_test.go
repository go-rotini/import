package kong_test

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/kong"
	imp "github.com/go-rotini/import"
	kongimport "github.com/go-rotini/import/kong"
)

// tagHas reports whether the Kong under test has the named field on its Tag, for features
// newer releases added.
func tagHas(name string) bool {
	_, ok := reflect.TypeOf(kong.Tag{}).FieldByName(name)
	return ok
}

func importGrammar(t *testing.T, grammar any, opts kongimport.Options) *imp.Result {
	t.Helper()
	if opts.Name == "" {
		opts.Name = "app"
	}
	res, err := kongimport.FromKong(grammar, opts)
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

func TestFromKongErrors(t *testing.T) {
	if _, err := kongimport.FromKong(nil, kongimport.Options{}); err == nil {
		t.Fatal("want an error for no grammar")
	}
	var notStruct int
	if _, err := kongimport.FromKong(&notStruct, kongimport.Options{}); err == nil {
		t.Fatal("want an error for a grammar that is not a struct")
	}
	boom := kong.OptionFunc(func(*kong.Kong) error { panic("boom") })
	_, err := kongimport.FromKong(&struct{}{}, kongimport.Options{KongOptions: []kong.Option{boom}})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("want the panic as an error, got %v", err)
	}
}

func TestName(t *testing.T) {
	res, err := kongimport.FromKong(&struct{}{}, kongimport.Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSuffix(strings.TrimSuffix(lastElem(os.Args[0]), ".exe"), ".test")
	if res.Command.Name != want {
		t.Fatalf("root name %q, want the test binary's %q", res.Command.Name, want)
	}
	res = importGrammar(t, &struct{}{}, kongimport.Options{Name: "my tool"})
	if res.Command.Name != "my-tool" {
		t.Fatalf("root name %q", res.Command.Name)
	}
	if res.Source.Framework != "kong" {
		t.Fatalf("framework %q", res.Source.Framework)
	}
}

func lastElem(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

type hookCmd struct {
	Name string `arg:""`
}

func (h *hookCmd) BeforeApply() error { return nil }
func (h *hookCmd) Run() error         { return nil }

type branchCmd struct {
	Leaf hookCmd `cmd:""`
}

func (b *branchCmd) AfterApply() error { return nil }

type embeddedHooks struct{}

func (embeddedHooks) BeforeReset() error { return nil }

type withEmbedded struct {
	embeddedHooks
	X bool
}

func TestCommandsAndHooks(t *testing.T) {
	var grammar struct {
		Branch branchCmd    `cmd:"" aliases:"b,b.x" help:"A branch.\nMore about it." group:"Admin" hidden:""`
		Emb    withEmbedded `cmd:"" name:"emb"`
	}
	res := importGrammar(t, &grammar, kongimport.Options{})
	b := find(t, res, "app branch")
	if b.Summary != "A branch." || b.Description != "A branch.\nMore about it." || b.Group != "Admin" || !b.Hidden {
		t.Fatalf("branch = %+v", b)
	}
	if !reflect.DeepEqual(b.Aliases, []string{"b"}) {
		t.Fatalf("aliases %v", b.Aliases)
	}
	wantNote(t, res, "app branch", imp.Lossy, `alias "b.x" dropped`)
	wantNote(t, res, "app", imp.Info, "Kong rejects a command that has sub-commands when none is named")
	leaf := find(t, res, "app branch leaf")
	if len(leaf.Arguments) != 1 || leaf.Arguments[0].Name != "name" || !leaf.Arguments[0].Schema.Required {
		t.Fatalf("leaf arguments %+v", leaf.Arguments)
	}
	var got []string
	for _, h := range res.Hooks {
		got = append(got, h.Path+" "+h.Framework+"->"+h.Rotini)
		if h.File == "" || h.Line == 0 || h.Func == "" {
			t.Errorf("hook %+v has no position", h)
		}
	}
	want := []string{
		"app branch AfterApply->CascadingPreRun",
		"app branch leaf BeforeApply->PreRun",
		"app branch leaf Run->Run",
		"app emb BeforeReset->PreRun",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hooks %v, want %v", got, want)
	}
	if !strings.HasSuffix(res.Hooks[3].Func, "embeddedHooks.BeforeReset") {
		t.Fatalf("embedded hook %q", res.Hooks[3].Func)
	}
}

type level int

func (l *level) Decode(*kong.DecodeContext) error { return nil }

func TestFlagTypes(t *testing.T) {
	var grammar struct {
		Count    int                `type:"counter" short:"v"`
		Str      string             `default:"x" placeholder:"S" help:"a string"`
		I8       int8               `name:"i.8" default:"-3"`
		U        uint               `default:"0x10"`
		F        float32            `default:"1.5"`
		B        bool               `default:"true"`
		D        time.Duration      `default:"1m"`
		T        time.Time          `format:"2006-01-02"`
		URL      *url.URL           `name:"url"`
		File     *os.File           `name:"file"`
		Strs     []string           `default:"a,b"`
		Ints     []int              `sep:";" default:"1;2"`
		NoSep    []string           `sep:"none" default:"a,b"`
		Map      map[string]int     `default:"a=1;b=2"`
		Path     string             `type:"path"`
		ExistF   string             `type:"existingfile"`
		ExistD   []string           `type:"existingdir"`
		Content  []byte             `type:"filecontent"`
		Custom   string             `type:"custom"`
		Level    level              `name:"level"`
		Ptr      *int               `name:"ptr" default:"4"`
		Config   kong.ConfigFlag    `name:"config"`
		Chdir    kong.ChangeDirFlag `name:"chdir"`
		Version  kong.VersionFlag   `name:"version"`
		Required string             `required:"" default:"d"`
	}
	res := importGrammar(t, &grammar, kongimport.Options{
		KongOptions: []kong.Option{kong.NamedMapper("custom", kong.MapperFunc(func(*kong.DecodeContext, reflect.Value) error { return nil }))},
	})
	root := res.Command
	cases := []struct {
		flag, typ string
		def       any
		sep       string
	}{
		{"count", "count", nil, ""},
		{"str", "string", "x", ""},
		{"i-8", "int8", int64(-3), ""},
		{"u", "uint", uint64(16), ""},
		{"f", "float32", 1.5, ""},
		{"b", "bool", true, ""},
		{"d", "duration", "1m", ""},
		{"t", "time", nil, ""},
		{"url", "url", nil, ""},
		{"file", "inputfile", nil, ""},
		{"strs", "[]string", []any{"a", "b"}, ","},
		{"ints", "[]int", []any{int64(1), int64(2)}, ";"},
		{"no-sep", "[]string", []any{"a,b"}, ""},
		{"map", "map[string]int", map[string]any{"a": int64(1), "b": int64(2)}, ";"},
		{"path", "string", nil, ""},
		{"exist-f", "existingfile", nil, ""},
		{"exist-d", "[]existingdir", nil, ","},
		{"content", "string", nil, ""},
		{"custom", "string", nil, ""},
		{"level", "string", nil, ""},
		{"ptr", "int", int64(4), ""},
		{"config", "string", nil, ""},
		{"chdir", "existingdir", nil, ""},
		{"required", "string", nil, ""},
	}
	for _, tc := range cases {
		f := flagNamed(t, root, tc.flag)
		if f.Schema.Type != tc.typ || !reflect.DeepEqual(f.Schema.Default, tc.def) || f.Schema.Separator != tc.sep {
			t.Errorf("--%s: type %q default %#v separator %q; want %q %#v %q", tc.flag, f.Schema.Type, f.Schema.Default, f.Schema.Separator, tc.typ, tc.def, tc.sep)
		}
	}
	if s := flagNamed(t, root, "str"); s.Summary != "a string" || s.Schema.Placeholder != "S" {
		t.Errorf("--str = %+v", s)
	}
	if c := flagNamed(t, root, "count"); !reflect.DeepEqual(c.Identifiers, []string{"-v", "--count"}) {
		t.Errorf("--count identifiers %v", c.Identifiers)
	}
	if p := flagNamed(t, root, "path"); p.Schema.Complete == nil || p.Schema.Complete.Kind != "file" {
		t.Errorf("--path complete %+v", p.Schema.Complete)
	}
	if r := flagNamed(t, root, "required"); !r.Schema.Required {
		t.Error("--required is not required")
	}
	v := flagNamed(t, root, "version")
	if v.Builtin != "version" || !v.ShortCircuit || v.Cascading {
		t.Errorf("--version = %+v", v)
	}
	h := root.Flags[0]
	if h.Builtin != "help" || !h.Cascading || !reflect.DeepEqual(h.Identifiers, []string{"-h", "--help"}) {
		t.Errorf("help flag = %+v", h)
	}
	for _, n := range []struct {
		level imp.Level
		msg   string
	}{
		{imp.Lossy, "flag --i.8 is not a valid rotini name; renamed to --i-8"},
		{imp.Lossy, "--t parses times with the layout"},
		{imp.Lossy, "--file opens the named file"},
		{imp.Lossy, "--path is a Kong path"},
		{imp.Lossy, "--content reads the named file's content"},
		{imp.Lossy, `--custom uses the Kong mapper "custom"`},
		{imp.Lossy, "--level has the custom type"},
		{imp.Lossy, "--config is a kong.ConfigFlag"},
		{imp.Lossy, "--chdir is a kong.ChangeDirFlag"},
		{imp.Info, "--version prints the Kong variable ${version}"},
		{imp.Lossy, "--required is required and has the default d"},
	} {
		wantNote(t, res, "app", n.level, n.msg)
	}
}

type traceFlag bool

func (traceFlag) BeforeApply() error { return nil }

func TestFlagDetails(t *testing.T) {
	var grammar struct {
		Out   string    `short:"o" aliases:"x,output-file,b.x" env:"APP_OUT,bad-env" group:"Output" hidden:""`
		Zero  string    `short:"1"`
		Mode  string    `enum:"a, b" default:"a"`
		Modes []string  `enum:"a,b"`
		Trace traceFlag `name:"trace"`
		Sub   struct {
			Arg string `arg:""`
		} `cmd:""`
	}
	res := importGrammar(t, &grammar, kongimport.Options{})
	root := res.Command
	out := flagNamed(t, root, "out")
	if !reflect.DeepEqual(out.Identifiers, []string{"-o", "--out", "-x", "--output-file"}) || out.Group != "Output" || !out.Hidden || !out.Cascading {
		t.Fatalf("--out = %+v", out)
	}
	if !reflect.DeepEqual(out.Schema.Variable, []string{"APP_OUT"}) {
		t.Fatalf("--out variables %v", out.Schema.Variable)
	}
	wantNote(t, res, "app", imp.Lossy, `alias "b.x" dropped from --out`)
	wantNote(t, res, "app", imp.Lossy, `environment variable "bad-env"`)
	wantNote(t, res, "app", imp.Lossy, "-1 dropped from --zero")
	mode := flagNamed(t, root, "mode")
	if !reflect.DeepEqual(mode.Schema.Enum, []imp.EnumValue{{Value: "a"}, {Value: "b"}}) {
		t.Fatalf("--mode enum %v", mode.Schema.Enum)
	}
	modes := flagNamed(t, root, "modes")
	if modes.Schema.Items == nil || len(modes.Schema.Items.Enum) != 2 || modes.Schema.Enum != nil {
		t.Fatalf("--modes schema %+v", modes.Schema)
	}
	wantNote(t, res, "app", imp.Lossy, "--trace runs")
	var hook *imp.Hook
	for i := range res.Hooks {
		if res.Hooks[i].Framework == "--trace BeforeApply" {
			hook = &res.Hooks[i]
		}
	}
	if hook == nil || hook.Rotini != "PreRun" {
		t.Fatalf("no --trace hook: %+v", res.Hooks)
	}
}

func TestNegatable(t *testing.T) {
	var grammar struct {
		Wait bool `negatable:""`
	}
	res := importGrammar(t, &grammar, kongimport.Options{})
	if n := flagNamed(t, res.Command, "wait").Schema.Negatable; n != true {
		t.Fatalf("--wait negatable %#v", n)
	}
	if reflect.TypeOf(kong.Tag{}.Negatable).Kind() != reflect.String {
		return // this Kong has no custom negated names
	}
	var custom struct {
		Color bool `negatable:"plain"`
	}
	res = importGrammar(t, &custom, kongimport.Options{})
	if n := flagNamed(t, res.Command, "color").Schema.Negatable; n != "--plain" {
		t.Fatalf("--color negatable %#v", n)
	}
}

func TestGroups(t *testing.T) {
	var grammar struct {
		JSON bool   `xor:"format"`
		YAML bool   `xor:"format"`
		Last bool   `xor:"rev" required:""`
		To   string `xor:"rev" required:""`
		A    bool   `xor:"half" required:""`
		B    bool   `xor:"half"`
		Solo bool   `xor:"solo"`
		Root bool   `xor:"spans"`
		Sub  struct {
			Child bool `xor:"spans"`
		} `cmd:""`
	}
	res := importGrammar(t, &grammar, kongimport.Options{})
	root := res.Command
	var got []string
	for _, g := range root.FlagGroups {
		got = append(got, g.Kind+" "+strings.Join(g.Flags, ","))
	}
	want := []string{"mutually_exclusive json,yaml", "one_of last,to", "one_of a,b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("groups %v, want %v", got, want)
	}
	for _, f := range []string{"last", "to", "a"} {
		if flagNamed(t, root, f).Schema.Required {
			t.Errorf("--%s is still required inside a one_of group", f)
		}
	}
	wantNote(t, res, "app", imp.Lossy, `xor group "half" has required and optional flags`)
	wantNote(t, res, "app", imp.Unsupported, `xor group "spans" spans flags of more than one command`)
	wantNote(t, res, "app sub", imp.Unsupported, `xor group "spans" spans flags of more than one command`)

	if !tagHas("And") {
		return
	}
	type andGrammar struct {
		Cert string `and:"tls" required:""`
		Key  string `and:"tls"`
	}
	res = importGrammar(t, &andGrammar{}, kongimport.Options{})
	g := res.Command.FlagGroups
	if len(g) != 1 || g[0].Kind != "required_together" || !reflect.DeepEqual(g[0].Flags, []string{"cert", "key"}) {
		t.Fatalf("and groups %+v", g)
	}
	if !flagNamed(t, res.Command, "key").Schema.Required {
		t.Fatal("--key is not required with its and group")
	}
}

func TestArguments(t *testing.T) {
	var grammar struct {
		Copy struct {
			Src  []string `arg:"" help:"Sources."`
			Opt  string   `arg:"" optional:""`
			Dflt int      `arg:"" default:"3"`
		} `cmd:""`
		Exec struct {
			Args []string `arg:""`
		} `cmd:"" passthrough:""`
		SSH struct {
			Node    string   `arg:""`
			Command []string `arg:"" optional:"" passthrough:""`
		} `cmd:"" name:"ssh"`
	}
	_, err := kongimport.FromKong(&grammar, kongimport.Options{Name: "app"})
	if err == nil {
		t.Fatal("want Kong's error for an argument after a cumulative one")
	}

	var ok struct {
		Copy struct {
			Opt  string   `arg:"" optional:""`
			Dflt int      `arg:"" default:"3"`
			Src  []string `arg:"" optional:"" sep:"," help:"Sources."`
		} `cmd:""`
		Exec struct {
			Args []string `arg:""`
		} `cmd:"" passthrough:""`
		SSH struct {
			Node    string   `arg:""`
			Command []string `arg:"" optional:"" passthrough:""`
		} `cmd:"" name:"ssh"`
		Default struct{} `cmd:"" default:"1"`
	}
	res := importGrammar(t, &ok, kongimport.Options{})
	cp := find(t, res, "app copy")
	if len(cp.Arguments) != 3 {
		t.Fatalf("copy arguments %+v", cp.Arguments)
	}
	if a := cp.Arguments[0]; a.Name != "opt" || a.Schema.Required {
		t.Errorf("<opt> = %+v", a.Schema)
	}
	if a := cp.Arguments[1]; a.Schema.Default != int64(3) || a.Schema.Required {
		t.Errorf("<dflt> = %+v", a.Schema)
	}
	if a := cp.Arguments[2]; a.Schema.Type != "[]string" || a.Schema.Separator != "," || a.Summary != "Sources." || a.Schema.Required || a.Schema.MinItems != nil {
		t.Errorf("<src> = %+v", a.Schema)
	}
	ex := find(t, res, "app exec")
	if !ex.Passthrough || len(ex.Arguments) != 1 || ex.Arguments[0].Passthrough {
		t.Errorf("exec = %+v", ex)
	}
	ssh := find(t, res, "app ssh")
	if ssh.Passthrough || !ssh.Arguments[1].Passthrough || ssh.Arguments[1].Schema.Separator != "" {
		t.Errorf("ssh = %+v %+v", ssh, ssh.Arguments[1])
	}
	wantNote(t, res, "app", imp.Unsupported, `default command "default" dropped`)
}

func TestBranchingArgument(t *testing.T) {
	var grammar struct {
		User struct {
			User   string   `arg:""`
			Delete struct{} `cmd:""`
		} `arg:""`
	}
	res := importGrammar(t, &grammar, kongimport.Options{})
	if find(t, res, "app user delete") == nil {
		t.Fatal("no user command")
	}
	wantNote(t, res, "app user", imp.Unsupported, "branching argument <user>")
}

func TestVars(t *testing.T) {
	var grammar struct {
		Region string   `help:"Region (${region}, ${known})." default:"${region}" env:"${env_name}" placeholder:"${region}"`
		Mode   string   `enum:"${modes}" default:"a"`
		Known  string   `default:"${known}"`
		Cmd    struct{} `cmd:"" help:"In ${region}."`
	}
	res := importGrammar(t, &grammar, kongimport.Options{Vars: map[string]string{"known": "k"}})
	root := res.Command
	r := flagNamed(t, root, "region")
	if r.Summary != "Region (${region}, k)." || r.Schema.Default != nil || r.Schema.Variable != nil || r.Schema.Placeholder != "${region}" {
		t.Fatalf("--region = %+v %+v", r, r.Schema)
	}
	if m := flagNamed(t, root, "mode"); m.Schema.Enum != nil {
		t.Fatalf("--mode enum %v", m.Schema.Enum)
	}
	if k := flagNamed(t, root, "known"); k.Schema.Default != "k" {
		t.Fatalf("--known default %v", k.Schema.Default)
	}
	if c := find(t, res, "app cmd"); c.Summary != "In ${region}." {
		t.Fatalf("cmd summary %q", c.Summary)
	}
	wantNote(t, res, "app", imp.Lossy, "Kong variables ${env_name}, ${modes}, ${region} were not supplied")
	wantNote(t, res, "app", imp.Lossy, "--region: its default comes from a Kong variable")
	wantNote(t, res, "app", imp.Lossy, "--mode: its enum comes from a Kong variable")
}

func TestKongOptions(t *testing.T) {
	var grammar struct {
		Cmd struct{} `cmd:"" group:"a"`
	}
	res := importGrammar(t, &grammar, kongimport.Options{KongOptions: []kong.Option{
		kong.Description("Does things."),
		kong.ExplicitGroups([]kong.Group{{Key: "a", Title: "Group A"}}),
		kong.NoDefaultHelp(),
	}})
	if res.Command.Summary != "Does things." {
		t.Fatalf("summary %q", res.Command.Summary)
	}
	if g := find(t, res, "app cmd").Group; g != "Group A" {
		t.Fatalf("group %q", g)
	}
	if len(res.Command.Flags) != 0 {
		t.Fatalf("flags without the default help: %+v", res.Command.Flags)
	}
}

func TestCounterDefaultAndEscapedSeparator(t *testing.T) {
	var grammar struct {
		Count int      `type:"counter" default:"2"`
		List  []string `default:"a\\,b,c"`
	}
	res := importGrammar(t, &grammar, kongimport.Options{})
	if got := flagNamed(t, res.Command, "count").Schema.Default; got != int64(2) {
		t.Errorf("--count default %#v", got)
	}
	if got := flagNamed(t, res.Command, "list").Schema.Default; !reflect.DeepEqual(got, []any{"a,b", "c"}) {
		t.Errorf("--list default %#v", got)
	}

	// Kong reads a counter's default in base 10.
	var bases struct {
		Octal int `type:"counter" default:"010"`
		Hex   int `type:"counter" default:"0x10"`
	}
	res = importGrammar(t, &bases, kongimport.Options{})
	if got := flagNamed(t, res.Command, "octal").Schema.Default; got != int64(10) {
		t.Errorf("--octal default %#v", got)
	}
	if got := flagNamed(t, res.Command, "hex").Schema.Default; got != nil {
		t.Errorf("--hex default %#v", got)
	}
	wantNote(t, res, "app", imp.Lossy, `--hex: the counter default "0x10" is not a decimal whole number`)
}

func TestCommandNamesThatCollideOnceValid(t *testing.T) {
	var grammar struct {
		Dotted struct{} `cmd:"" name:"a.b"`
		Dashed struct{} `cmd:"" name:"a-b"`
	}
	res := importGrammar(t, &grammar, kongimport.Options{})
	if got := names(res.Command.Commands); len(got) != 2 || got[0] == got[1] {
		t.Fatalf("commands %v", got)
	}
}

func names(cs []*imp.Command) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return out
}

func TestRenamedFlagDoesNotShadowInherited(t *testing.T) {
	var grammar struct {
		FooBar string `name:"foo-bar"`
		Sub    struct {
			Dotted string `name:"foo.bar"`
		} `cmd:""`
	}
	res := importGrammar(t, &grammar, kongimport.Options{})
	flagNamed(t, find(t, res, "app sub"), "foo-bar-2")
	wantNote(t, res, "app sub", imp.Lossy, "would be --foo-bar, the name of an inherited flag")
}
