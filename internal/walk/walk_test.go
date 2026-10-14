package walk

import (
	"errors"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"

	imp "github.com/go-rotini/import"
)

func TestParseUse(t *testing.T) {
	cases := []struct {
		use  string
		want []UseArg
	}{
		{"deploy", nil},
		{"deploy <service>", []UseArg{{Name: "service"}}},
		{"rollback <service> [revision]", []UseArg{{Name: "service"}, {Name: "revision", Optional: true}}},
		{"delete NAME...", []UseArg{{Name: "name", Variadic: true}}},
		{"cp <src>... <dst>", []UseArg{{Name: "src", Variadic: true}, {Name: "dst"}}},
		{"run [flags] IMAGE [COMMAND] [ARG...]", []UseArg{{Name: "image"}, {Name: "command", Optional: true}, {Name: "arg", Optional: true, Variadic: true}}},
		{"x [global flags] [options...]", nil},
		{"exec -- CMD [ARGS...]", []UseArg{{Name: "cmd"}, {Name: "args", Optional: true, Variadic: true}}},
		{"trace [trace directory]", []UseArg{{Name: "trace", Optional: true}}},
		{"get [(-o|--output=)json|yaml] (TYPE[.VERSION][.GROUP] [NAME | -l label] | TYPE[.VERSION][.GROUP]/NAME ...) [flags]",
			[]UseArg{{Name: "type", Variadic: true}}},
		{"describe (-f FILENAME | TYPE [NAME_PREFIX | -l label] | TYPE/NAME)", []UseArg{{Name: "type", Optional: true, Choice: true}}},
		{"pick {<number> | <url>}", []UseArg{{Name: "number"}}},
		{"show <repo-id|repo-full-name> [pipeline]", []UseArg{{Name: "repo_id"}, {Name: "pipeline", Optional: true}}},
		{"annotate (-f FILENAME | TYPE NAME) KEY_1=VAL_1 ... KEY_N=VAL_N", []UseArg{{Name: "type", Optional: true, Choice: true}, {Name: "key_1", Variadic: true}}},
		{"cat FILE1 ... FILEN", []UseArg{{Name: "file1", Variadic: true}}},
		{"cat FILE ... FILEN", []UseArg{{Name: "file", Variadic: true}}},
		{"run ARG ... ARGN", []UseArg{{Name: "arg", Variadic: true}}},
		{"grep PATTERN ... PATTERNN", []UseArg{{Name: "pattern", Variadic: true}}},
		{"cp SRC ... DST", []UseArg{{Name: "src", Variadic: true}, {Name: "dst"}}},
		{"logs [-f] [-p] (POD | TYPE/NAME)", []UseArg{{Name: "pod"}}},
		{"x [-o json|yaml] <a>", []UseArg{{Name: "a"}}},
		{"set-context [NAME | --current] [--cluster=cluster_nickname]", []UseArg{{Name: "name", Optional: true}}},
		{"port-forward TYPE/NAME [options] [LOCAL_PORT:]REMOTE_PORT [...[LOCAL_PORT_N:]REMOTE_PORT_N]",
			[]UseArg{{Name: "type"}, {Name: "local_port"}, {Name: "local_port_n", Optional: true, Variadic: true}}},
		{"x 2nd-thing", []UseArg{{Name: "arg_2nd_thing"}}},
		{"x\tA\nB", []UseArg{{Name: "a"}, {Name: "b"}}},
	}
	for _, tc := range cases {
		if got := ParseUse(tc.use); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ParseUse(%q) = %+v, want %+v", tc.use, got, tc.want)
		}
	}
}

func TestArgName(t *testing.T) {
	cases := map[string]string{
		"NAME":        "name",
		"local-port":  "local_port",
		"a.b/c":       "a_b_c",
		"9lives":      "arg_9lives",
		"--":          "",
		"Remote_PORT": "remote_port",
	}
	for in, want := range cases {
		if got := ArgName(in); got != want {
			t.Errorf("ArgName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUniqueNames(t *testing.T) {
	args := []*imp.Argument{{Name: "name"}, {Name: "name"}, {Name: "name_2"}, {Name: "name"}}
	UniqueNames(args)
	var got []string
	for _, a := range args {
		got = append(got, a.Name)
	}
	want := []string{"name", "name_2", "name_2_2", "name_3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCommandName(t *testing.T) {
	cases := map[string]string{
		"deploy":     "deploy",
		"config:set": "config-set",
		"9p":         "p",
		"":           "cmd",
		"--x":        "x",
		"get_all-2":  "get_all-2",
	}
	for in, want := range cases {
		if got := CommandName(in); got != want {
			t.Errorf("CommandName(%q) = %q, want %q", in, got, want)
		}
	}
	if ValidName("a.b") || !ValidName("a-b") {
		t.Error("validName")
	}
}

func TestUseFieldsUnbalanced(t *testing.T) {
	got := useFields("x a] [b c")
	want := []string{"x", "a]", "[b c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFuncHelpers(t *testing.T) {
	if FuncName(nil) != "" || FuncName(42) != "" {
		t.Error("funcName of a non-function")
	}
	var nilFn func()
	if name, _, _ := FuncPos(nilFn); name != "" {
		t.Error("funcPos of a nil function")
	}
	name, file, line := FuncPos(TestFuncHelpers)
	if name != "github.com/go-rotini/import/internal/walk.TestFuncHelpers" || file == "" || line == 0 {
		t.Errorf("funcPos = %q %q %d", name, file, line)
	}
	if !SameFunc(TestFuncHelpers, TestFuncHelpers) || SameFunc(TestFuncHelpers, TestCommandName) {
		t.Error("sameFunc")
	}
}

func TestParseArgsUsage(t *testing.T) {
	got := ParseArgsUsage("<src>... <dst> [flags]")
	want := []UseArg{{Name: "src", Variadic: true}, {Name: "dst"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseArgsUsage = %+v, want %+v", got, want)
	}
	if ParseArgsUsage("") != nil {
		t.Fatal("ParseArgsUsage of nothing")
	}
}

func TestSmallHelpers(t *testing.T) {
	if JoinPath("", "a") != "a" || JoinPath("a", "b") != "a b" {
		t.Error("JoinPath")
	}
	if got, _ := SplitExamples("  a \n\n b"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("SplitExamples = %q", got)
	}
	if s := Source("x", "example.com/none", strings.ToUpper); s.Framework != "x" || s.Version != "" {
		t.Errorf("Source = %+v", s)
	}
	if DependsOn("example.com/none") {
		t.Error("DependsOn a module that is not linked")
	}
}

func TestFileVersion(t *testing.T) {
	for _, tc := range []struct{ file, module, want string }{
		{"/home/me/go/pkg/mod/github.com/spf13/cobra@v1.8.0/args.go", "github.com/spf13/cobra", "v1.8.0"},
		{"github.com/spf13/cobra@v1.8.0/args.go", "github.com/spf13/cobra", "v1.8.0"},
		{`C:/Users/me/go/pkg/mod/github.com/!burnt!sushi/toml@v1.3.2/decode.go`, "github.com/BurntSushi/toml", "v1.3.2"},
		{"/go/pkg/mod/github.com/urfave/cli/v3@v3.1.0/help.go", "github.com/urfave/cli/v3", "v3.1.0"},
		// A longer module path that ends in the same words is not the module.
		{"/go/pkg/mod/example.com/x-github.com/spf13/cobra@v1.0.0/a.go", "github.com/spf13/cobra", ""},
		{"/src/cobra/args.go", "github.com/spf13/cobra", ""},
		{"/go/pkg/mod/github.com/spf13/cobra@v1.8.0", "github.com/spf13/cobra", ""},
	} {
		if got := fileVersion(tc.file, tc.module); got != tc.want {
			t.Errorf("fileVersion(%q, %q) = %q, want %q", tc.file, tc.module, got, tc.want)
		}
	}
}

func TestDefinedBeside(t *testing.T) {
	local := func() {}
	if !DefinedBeside(local, CommandName) || !DefinedBeside(JoinPath, CommandName) {
		t.Error("a function of this package is not beside another")
	}
	if DefinedBeside(strings.ToUpper, CommandName) || DefinedBeside(nil, CommandName) || DefinedBeside(local, nil) {
		t.Error("DefinedBeside matched another package or nil")
	}
}

func TestSplitExamplesJoinsCommands(t *testing.T) {
	block := "  # create one\n  app create \\\n    --name x\n" +
		"  app api graphql -f query='\n    query { viewer }\n  '\n" +
		"  app say \"two\n  lines\"\n" +
		"  Don't run this as root\n  app run\n"
	ex, comments := SplitExamples(block)
	want := []string{
		"app create \\\n--name x",
		"app api graphql -f query='\nquery { viewer }\n'",
		"app say \"two\nlines\"",
		"Don't run this as root",
		"app run",
	}
	if !reflect.DeepEqual(ex, want) {
		t.Errorf("examples %q, want %q", ex, want)
	}
	if !reflect.DeepEqual(comments, []string{"create one"}) {
		t.Errorf("comments %q", comments)
	}
	// Two apostrophes in prose do not pair up into a quote across lines.
	ex, _ = SplitExamples("app say don't\napp ls\napp say won't\napp x --m=\"a\nb\"")
	if want := []string{"app say don't", "app ls", "app say won't", "app x --m=\"a\nb\""}; !reflect.DeepEqual(ex, want) {
		t.Errorf("examples %q, want %q", ex, want)
	}
}

func TestModulePath(t *testing.T) {
	t.Setenv("GOMODCACHE", "/cache/mod")
	cases := map[string]string{
		"/cache/mod/example.com/x@v1.2.3/cmd.go":           "example.com/x@v1.2.3/cmd.go",
		"/home/me/repo/pkg/mod/app/x.go":                   "/home/me/repo/pkg/mod/app/x.go",
		"/other/go/pkg/mod/example.com/y@v0.1.0/y.go":      "example.com/y@v0.1.0/y.go",
		"/home/me/repo/pkg/mod/example.com/z@v1/z.go":      "example.com/z@v1/z.go",
		"/home/me/repo/cmd/main.go":                        "/home/me/repo/cmd/main.go",
		"/home/me/pkg/mod/x/pkg/mod/example.com/q@v1/q.go": "example.com/q@v1/q.go",
	}
	for in, want := range cases {
		if got := ModulePath(in); got != want {
			t.Errorf("ModulePath(%q) = %q, want %q", in, got, want)
		}
	}
}

type receiver struct{ n int }

func (r *receiver) small() error {
	if r.n > 0 {
		return errors.New("set")
	}
	return nil
}

func (r *receiver) empty() error { return nil }

//go:noinline
func (r *receiver) large() error {
	for i := range r.n {
		_ = i
	}
	return nil
}

func TestFuncPosMethodValue(t *testing.T) {
	r := &receiver{}
	for _, tc := range []struct {
		fn   any
		name string
	}{
		{r.small, "github.com/go-rotini/import/internal/walk.(*receiver).small"},
		{r.large, "github.com/go-rotini/import/internal/walk.(*receiver).large"},
	} {
		name, file, line := FuncPos(tc.fn)
		if name != tc.name || !strings.HasSuffix(file, "walk_test.go") || line == 0 {
			t.Errorf("FuncPos = %q %q %d, want %q in walk_test.go", name, file, line, tc.name)
		}
	}
	// A method that compiles to nothing leaves no trace to find; it is named, not located.
	if name, file, line := FuncPos(r.empty); name != "github.com/go-rotini/import/internal/walk.(*receiver).empty" || file != "" || line != 0 {
		t.Errorf("FuncPos(empty) = %q %q %d", name, file, line)
	}
}

func TestMachineScrub(t *testing.T) {
	m := &Machine{
		dirs:     []machineDir{{"/home/me/.cache", "the user cache directory", "<cache>"}, {"/home/me", "the home directory", "~"}},
		hosts:    []string{"buildbox"},
		testName: "app.test",
		name:     "app",
	}
	root := &imp.Command{
		Name:        "app",
		Description: "Run `eval \"$(app.test completion bash)\"`; app.tests stays.",
		Flags: []*imp.Flag{
			{Name: "config", Schema: &imp.Schema{Type: "string", Default: "/home/me/.config/app.yaml"}},
			{Name: "cache", Schema: &imp.Schema{Type: "[]string", Default: []any{"/tmp", "/home/me/.cache/app"}}},
			{Name: "host", Schema: &imp.Schema{Type: "string", Default: "buildbox"}},
			{Name: "homes", Schema: &imp.Schema{Type: "string", Default: "/home/meal"}},
			{Name: "prog", Summary: "see app.test help", Schema: &imp.Schema{Type: "string", Default: "app.test"}},
		},
	}
	var notes []string
	m.Scrub(root, "app", func(path string, level imp.Level, msg string) {
		notes = append(notes, string(level)+" "+path+": "+msg)
	})
	if want := "Run `eval \"$(app completion bash)\"`; app.tests stays."; root.Description != want {
		t.Errorf("description %q", root.Description)
	}
	for _, f := range root.Flags[:3] {
		if f.Schema.Default != nil {
			t.Errorf("--%s kept the default %v", f.Name, f.Schema.Default)
		}
	}
	if root.Flags[3].Schema.Default != "/home/meal" {
		t.Errorf("--homes default %v", root.Flags[3].Schema.Default)
	}
	if root.Flags[4].Schema.Default != "app" || root.Flags[4].Summary != "see app help" {
		t.Errorf("--prog %q %v", root.Flags[4].Summary, root.Flags[4].Schema.Default)
	}
	joined := strings.Join(notes, "\n")
	for _, want := range []string{
		`--config: the default "~/.config/app.yaml" holds the home directory`,
		"--cache: the default \"[/tmp <cache>/app]\" holds the user cache directory",
		"--host: the default \"<host>\" holds the host name",
		`the test binary's name "app.test"`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("no note containing %q in:\n%s", want, joined)
		}
	}
	if len(notes) != 4 {
		t.Errorf("%d notes:\n%s", len(notes), joined)
	}
}

func TestHostNames(t *testing.T) {
	for host, want := range map[string][]string{
		"localhost":                       nil,
		"localhost.localdomain":           nil,
		"ubuntu":                          nil,
		"server":                          nil,
		"ubuntu.example.com":              {"ubuntu.example.com"},
		"ip-10-0-0-7.ec2.internal":        {"ip-10-0-0-7.ec2.internal", "ip-10-0-0-7"},
		"Matts-MacBook-Pro.local":         {"Matts-MacBook-Pro.local", "Matts-MacBook-Pro"},
		"buildbox7":                       {"buildbox7"},
		"box.example.com":                 {"box.example.com"},
		"localhost.localdomain.something": nil,
		"2024":                            nil,
		"10.0.0.7":                        nil,
	} {
		if got := hostNames(host); !reflect.DeepEqual(got, want) {
			t.Errorf("hostNames(%q) = %q, want %q", host, got, want)
		}
	}
	m := &Machine{hosts: hostNames("localhost.localdomain")}
	for _, s := range []string{"localhost:8080", "http://localhost:9090/metrics"} {
		if w := m.find(s); w != "" {
			t.Errorf("find(%q) = %q", s, w)
		}
	}
	m = &Machine{hosts: hostNames("ubuntu")}
	if w := m.find("ubuntu:22.04"); w != "" {
		t.Errorf("find(ubuntu:22.04) = %q", w)
	}
}

func TestMachineSharedTemp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("TMPDIR does not set the temporary directory on Windows")
	}
	t.Setenv("TMPDIR", "/tmp")
	if w := NewMachine("app").find("/tmp/app"); w != "" {
		t.Errorf("/tmp/app with TMPDIR=/tmp: %q", w)
	}
	t.Setenv("TMPDIR", "/var/folders/xy/T")
	if w := NewMachine("app").find("/var/folders/xy/T/app"); w != "the temporary directory" {
		t.Errorf("a per-user temporary directory: %q", w)
	}
}

func TestMachineScrubHelpText(t *testing.T) {
	m := &Machine{
		dirs:  []machineDir{{"/home/me", "the home directory", "~"}},
		hosts: []string{"buildbox-7"},
		name:  "app",
	}
	sub := &imp.Command{Name: "sub", Summary: "plain"}
	root := &imp.Command{
		Name:        "app",
		Summary:     "reads /home/me/.app",
		Description: "runs on buildbox-7; /home/meal stays",
		Examples:    []string{"app --config /home/me/x.yaml"},
		Flags: []*imp.Flag{
			{Name: "config", Summary: "config file (default /home/me/.app.yaml)", Schema: &imp.Schema{Type: "string", ImplicitValue: "/home/me/.app.yaml"}},
			{Name: "mode", Schema: &imp.Schema{Type: "string", ImplicitValue: "fast", Enum: []imp.EnumValue{{Value: "a", Summary: "uses /home/me/a"}}}},
		},
		Arguments: []*imp.Argument{{Name: "dir", Summary: "under /home/me", Schema: &imp.Schema{Type: "string"}}},
		Commands:  []*imp.Command{sub},
	}
	var notes []string
	m.Scrub(root, "app", func(path string, level imp.Level, msg string) {
		notes = append(notes, string(level)+" "+path+": "+msg)
	})
	for _, tc := range [][2]string{
		{root.Summary, "reads ~/.app"},
		{root.Description, "runs on <host>; /home/meal stays"},
		{root.Examples[0], "app --config ~/x.yaml"},
		{root.Flags[0].Summary, "config file (default ~/.app.yaml)"},
		{root.Flags[1].Schema.Enum[0].Summary, "uses ~/a"},
		{root.Arguments[0].Summary, "under ~"},
		{root.Flags[1].Schema.ImplicitValue, "fast"},
		{root.Flags[0].Schema.ImplicitValue, ""},
	} {
		if tc[0] != tc[1] {
			t.Errorf("got %q, want %q", tc[0], tc[1])
		}
	}
	want := []string{
		`lossy app: --config: the value "~/.app.yaml" it takes when given alone holds the home directory of the machine the import ran on, so it was dropped and the flag needs a value; set it in the handler`,
		`lossy app: help text held the home directory and the host name of the machine the import ran on; it was written as "~" and "<host>"`,
	}
	if !reflect.DeepEqual(notes, want) {
		t.Errorf("notes:\n%s", strings.Join(notes, "\n"))
	}
}

func TestMachineScrubWindowsTestBinary(t *testing.T) {
	// On Windows, help text that names the program from os.Args[0] holds the name with ".exe".
	m := &Machine{testName: "app.test", name: "app"}
	root := &imp.Command{Name: "app", Summary: "the app.test.exe tool", Examples: []string{"app.test.exe sub x"}}
	var notes []string
	m.Scrub(root, "app", func(path string, level imp.Level, msg string) {
		notes = append(notes, msg)
	})
	if root.Summary != "the app tool" || root.Examples[0] != "app sub x" {
		t.Errorf("summary %q, example %q", root.Summary, root.Examples[0])
	}
	if want := []string{`the test binary's name "app.test", which the program read from os.Args[0], was replaced by "app" in one command`}; !reflect.DeepEqual(notes, want) {
		t.Errorf("notes %q", notes)
	}
	if got := m.find("C:/x/app.test.exe"); got != "the test binary's name" {
		t.Errorf("find = %q", got)
	}
}

func TestNamesRename(t *testing.T) {
	n := Names{}
	for _, tc := range []struct{ in, name, why string }{
		{"foo.bar", "foo-bar", "is not a valid rotini name"},
		{"foo-bar", "foo-bar-2", `is "foo-bar" once made a valid rotini name, which another already has`},
		{"x", "x", ""},
	} {
		if name, why := n.Rename(tc.in); name != tc.name || why != tc.why {
			t.Errorf("Rename(%q) = %q, %q", tc.in, name, why)
		}
	}
}

func TestSharedHooks(t *testing.T) {
	hooks := []imp.Hook{
		{Path: "a x", Framework: "RunE", Func: "example.com/x.Wrap.func1"},
		{Path: "a y", Framework: "RunE", Func: "example.com/x.Wrap.func1"},
		{Path: "a z", Framework: "RunE", Func: "example.com/x.run"},
		{Path: "a w", Framework: "RunE", Func: "example.com/x.run"},
		{Path: "a v", Framework: "PreRunE", Func: "example.com/x.Wrap.func1"},
	}
	got := SharedHooks(hooks)
	if len(got) != 1 || !strings.Contains(got[0], "example.com/x.Wrap.func1 is the RunE of 2 commands") {
		t.Fatalf("SharedHooks = %q", got)
	}

	// A closure inlined into each caller is named after the caller, but written in one place.
	hooks = []imp.Hook{
		{Path: "a x", Framework: "RunE", Func: "example.com/x.init.Wrap.func3", File: "x/wrap.go", Line: 7},
		{Path: "a y", Framework: "RunE", Func: "example.com/x.init.Wrap.func4", File: "x/wrap.go", Line: 7},
		{Path: "a z", Framework: "RunE", Func: "example.com/x.init.func5", File: "x/wrap.go", Line: 12},
	}
	got = SharedHooks(hooks)
	if len(got) != 1 || !strings.Contains(got[0], "the closure at x/wrap.go:7 is the RunE of 2 commands") {
		t.Fatalf("SharedHooks of inlined closures = %q", got)
	}
}

func TestMachineSharedTempNote(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("TMPDIR does not set the temporary directory on Windows")
	}
	t.Setenv("TMPDIR", "/tmp")
	root := &imp.Command{Name: "app", Flags: []*imp.Flag{
		{Name: "dir", Schema: &imp.Schema{Type: "string", Default: "/tmp/app"}},
		{Name: "other", Schema: &imp.Schema{Type: "string", Default: "/tmpfoo/app"}},
	}}
	var notes []string
	NewMachine("app").Scrub(root, "app", func(path string, level imp.Level, msg string) {
		notes = append(notes, string(level)+" "+path+": "+msg)
	})
	if root.Flags[0].Schema.Default != "/tmp/app" {
		t.Errorf("--dir default %v", root.Flags[0].Schema.Default)
	}
	want := []string{`info app: --dir: the default "/tmp/app" is under the system temporary directory, which other systems put elsewhere; if the program asks the system for it, declare it with expand:`}
	if !reflect.DeepEqual(notes, want) {
		t.Errorf("notes:\n%s", strings.Join(notes, "\n"))
	}
}

func TestMachineScrubNotes(t *testing.T) {
	m := &Machine{
		dirs:     []machineDir{{"C:/Users/alice", "the home directory", "~"}, {"/home/me", "the home directory", "~"}},
		hosts:    []string{"buildbox-7"},
		testName: "app.test",
		testPath: "C:/Users/alice/AppData/Local/Temp/go-build1/b001/app.test.exe",
		name:     "app",
	}
	notes := []imp.Note{
		{Path: "app", Level: imp.Lossy, Msg: "--state is required and has the default /home/me/.app-state: dropped"},
		{Path: "app", Level: imp.Lossy, Msg: `comment lines in the examples dropped: ["write to /home/me/out on buildbox-7"]`},
		{Path: "app", Level: imp.Info, Msg: "run app.test help; /home/meal stays"},
		// Windows paths, as written and as %q quotes them.
		{Path: "app", Level: imp.Lossy, Msg: `comment lines in the examples dropped: ["write to C:\\Users\\alice\\out"]`},
		{Path: "app", Level: imp.Lossy, Msg: `the default C:\Users\alice\.app holds the home directory`},
		{Path: "app", Level: imp.Lossy, Msg: `the Use line "C:\\Users\\alice\\AppData\\Local\\Temp\\go-build1\\b001\\app.test.exe <a>" names no argument`},
	}
	m.ScrubNotes(notes)
	for i, want := range []string{
		"--state is required and has the default ~/.app-state: dropped",
		`comment lines in the examples dropped: ["write to ~/out on <host>"]`,
		"run app help; /home/meal stays",
		`comment lines in the examples dropped: ["write to ~\\out"]`,
		`the default ~\.app holds the home directory`,
		`the Use line "app <a>" names no argument`,
	} {
		if notes[i].Msg != want {
			t.Errorf("note %d: %q, want %q", i, notes[i].Msg, want)
		}
	}
}

func TestNamesRenameFlag(t *testing.T) {
	inherited := map[string]string{"foo-bar": "foo.bar", "same": "same", "log-level": "log-level"}
	n := Names{}
	for _, tc := range []struct{ in, name, why, shadowed string }{
		// A valid name overrides the inherited flag that was renamed to it.
		{"foo-bar", "foo-bar", "", "foo.bar"},
		// The same name as an inherited flag overrides it, as the framework does.
		{"same", "same", "", ""},
		// A name that had to be made valid does not take an inherited flag's name.
		{"log.level", "log-level-2", "would be --log-level, the name of an inherited flag", ""},
		{"x", "x", "", ""},
	} {
		if name, why, shadowed := n.RenameFlag(tc.in, inherited); name != tc.name || why != tc.why || shadowed != tc.shadowed {
			t.Errorf("RenameFlag(%q) = %q, %q, %q", tc.in, name, why, shadowed)
		}
	}
}

func TestProgramName(t *testing.T) {
	base := testBinary()
	if base == "" {
		t.Skip("not running as a go test binary")
	}
	if name, ok := ProgramName(base); !ok || name != strings.TrimSuffix(base, ".test") {
		t.Errorf("ProgramName(%q) = %q, %v", base, name, ok)
	}
	if name, ok := ProgramName("app"); ok || name != "app" {
		t.Errorf("ProgramName(app) = %q, %v", name, ok)
	}
}

func TestProgramNameFromPath(t *testing.T) {
	arg0 := os.Args[0]
	t.Cleanup(func() { os.Args[0] = arg0 })
	os.Args[0] = "/tmp/John Smith/go-build1/b001/app.test"
	for _, name := range []string{
		os.Args[0],
		"app.test",
		"app.test.exe",
		"/elsewhere/app.test",
		"/tmp/John", // a framework that cuts the usage line at the first space
	} {
		if got, ok := ProgramName(name); !ok || got != "app" {
			t.Errorf("ProgramName(%q) = %q, %v", name, got, ok)
		}
	}
	for _, name := range []string{"", "/tmp/John Smith/go-build1/b001", "app", "/tmp"} {
		if got, ok := ProgramName(name); ok || got != name {
			t.Errorf("ProgramName(%q) = %q, %v", name, got, ok)
		}
	}
	if got := CutProgram(os.Args[0] + " <a> [b]"); got != "app.test <a> [b]" {
		t.Errorf("CutProgram = %q", got)
	}
	if got := CutProgram(os.Args[0] + "x <a>"); got != os.Args[0]+"x <a>" {
		t.Errorf("CutProgram of another path = %q", got)
	}
	m := NewMachine("app")
	if got, _ := m.text("usage: " + os.Args[0] + " sub, or app.test sub"); got != "usage: app sub, or app sub" {
		t.Errorf("text = %q", got)
	}
}
