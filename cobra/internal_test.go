package cobra

import (
	"reflect"
	"testing"

	imp "github.com/go-rotini/import"
)

func TestParseUse(t *testing.T) {
	cases := []struct {
		use  string
		want []useArg
	}{
		{"deploy", nil},
		{"deploy <service>", []useArg{{name: "service"}}},
		{"rollback <service> [revision]", []useArg{{name: "service"}, {name: "revision", optional: true}}},
		{"delete NAME...", []useArg{{name: "name", variadic: true}}},
		{"cp <src>... <dst>", []useArg{{name: "src", variadic: true}, {name: "dst"}}},
		{"run [flags] IMAGE [COMMAND] [ARG...]", []useArg{{name: "image"}, {name: "command", optional: true}, {name: "arg", optional: true, variadic: true}}},
		{"x [global flags] [options...]", nil},
		{"exec -- CMD [ARGS...]", []useArg{{name: "cmd"}, {name: "args", optional: true, variadic: true}}},
		{"trace [trace directory]", []useArg{{name: "trace", optional: true}}},
		{"get [(-o|--output=)json|yaml] (TYPE[.VERSION][.GROUP] [NAME | -l label] | TYPE[.VERSION][.GROUP]/NAME ...) [flags]",
			[]useArg{{name: "type", variadic: true}}},
		{"describe (-f FILENAME | TYPE [NAME_PREFIX | -l label] | TYPE/NAME)", nil},
		{"set-context [NAME | --current] [--cluster=cluster_nickname]", []useArg{{name: "name", optional: true}}},
		{"port-forward TYPE/NAME [options] [LOCAL_PORT:]REMOTE_PORT [...[LOCAL_PORT_N:]REMOTE_PORT_N]",
			[]useArg{{name: "type"}, {name: "local_port"}, {name: "local_port_n", optional: true, variadic: true}}},
		{"x 2nd-thing", []useArg{{name: "arg_2nd_thing"}}},
		{"x\tA\nB", []useArg{{name: "a"}, {name: "b"}}},
	}
	for _, tc := range cases {
		if got := parseUse(tc.use); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("parseUse(%q) = %+v, want %+v", tc.use, got, tc.want)
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
		if got := argName(in); got != want {
			t.Errorf("argName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUniqueNames(t *testing.T) {
	args := []*imp.Argument{{Name: "name"}, {Name: "name"}, {Name: "name_2"}, {Name: "name"}}
	uniqueNames(args)
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
		if got := commandName(in); got != want {
			t.Errorf("commandName(%q) = %q, want %q", in, got, want)
		}
	}
	if validName("a.b") || !validName("a-b") {
		t.Error("validName")
	}
}

func TestDefaultValue(t *testing.T) {
	cases := []struct {
		typ, def string
		want     any
	}{
		{"string", "", nil},
		{"string", "[x]", "[x]"},
		{"bool", "false", nil},
		{"bool", "true", true},
		{"int", "0", nil},
		{"int", "-3", int64(-3)},
		{"uint64", "18446744073709551615", uint64(18446744073709551615)},
		{"float64", "0.25", 0.25},
		{"duration", "0s", nil},
		{"duration", "1m30s", "1m30s"},
		{"stringSlice", "[]", nil},
		{"stringSlice", `[a,"b,c"]`, []any{"a", "b,c"}},
		{"intSlice", "[1,2]", []any{int64(1), int64(2)}},
		{"boolSlice", "[true,false]", []any{true, false}},
		{"stringToString", "[a=1,b=x=y]", map[string]any{"a": "1", "b": "x=y"}},
		{"stringToInt", "[a=1]", map[string]any{"a": int64(1)}},
		{"ipNet", "<nil>", nil},
		{"time", "0001-01-01T00:00:00Z", nil},
		{"custom", "[]", nil},
		{"custom", "[a]", "[a]"},
	}
	for _, tc := range cases {
		if got := defaultValue(tc.typ, tc.def); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("defaultValue(%q, %q) = %#v, want %#v", tc.typ, tc.def, got, tc.want)
		}
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
	if funcName(nil) != "" || funcName(42) != "" {
		t.Error("funcName of a non-function")
	}
	var nilFn func()
	if name, _, _ := funcPos(nilFn); name != "" {
		t.Error("funcPos of a nil function")
	}
	name, file, line := funcPos(TestFuncHelpers)
	if name != "github.com/go-rotini/import/cobra.TestFuncHelpers" || file == "" || line == 0 {
		t.Errorf("funcPos = %q %q %d", name, file, line)
	}
	if !sameFunc(TestFuncHelpers, TestFuncHelpers) || sameFunc(TestFuncHelpers, TestDefaultValue) {
		t.Error("sameFunc")
	}
}
