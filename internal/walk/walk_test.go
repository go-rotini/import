package walk

import (
	"reflect"
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
		{"describe (-f FILENAME | TYPE [NAME_PREFIX | -l label] | TYPE/NAME)", nil},
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
	if got := SplitExamples("  a \n\n b"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("SplitExamples = %q", got)
	}
	if s := Source("x", "example.com/none"); s.Framework != "x" || s.Version != "" {
		t.Errorf("Source = %+v", s)
	}
	if DependsOn("example.com/none") {
		t.Error("DependsOn a module that is not linked")
	}
}
