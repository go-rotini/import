package kong

import (
	"testing"

	"github.com/alecthomas/kong"
)

// FuzzDefaultValue checks that any default text types without panicking and keeps one list
// item per part Kong splits it into, and that display text never keeps a variable placeholder.
func FuzzDefaultValue(f *testing.F) {
	for _, s := range []string{"", "1", "-3", "0x10", "1.5", "true", "a,b", "a=1;b=2", "=;=", "\uE000x\uE001", "\uE000\uE000x\uE001\uE001"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, def string) {
		for _, base := range []string{"int", "uint", "float64", "bool", "string"} {
			defaultValue("", base, def, ",", ";")
			defaultValue("map", base, def, ",", ";")
			if got, _ := defaultValue("slice", base, def, ",", ";").([]any); len(got) != len(kong.SplitEscaped(def, ',')) {
				t.Fatalf("defaultValue(%q) gave %d items", def, len(got))
			}
		}
		if got := text(def); placeholders.MatchString(got) {
			t.Fatalf("text(%q) = %q keeps a placeholder", def, got)
		}
	})
}
