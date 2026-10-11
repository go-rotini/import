package cobra

import (
	"reflect"
	"testing"
)

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
