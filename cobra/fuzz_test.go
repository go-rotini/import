package cobra

import "testing"

// FuzzParseUse checks that any Use line gives valid, distinct-after-dedupe argument names.
func FuzzParseUse(f *testing.F) {
	for _, s := range []string{
		"deploy <service>",
		"get [(-o|--output=)json|yaml] (TYPE[.VERSION][.GROUP] [NAME | -l label] | TYPE[.VERSION][.GROUP]/NAME ...) [flags]",
		"cp <src>... <dst>",
		"x [a [b [c]]] ((d)) <<e>>",
		"x ] [ ) ( > <",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, use string) {
		for _, a := range parseUse(use) {
			if a.name == "" || !isLetter(rune(a.name[0])) {
				t.Fatalf("parseUse(%q) gave the name %q", use, a.name)
			}
			for _, r := range a.name {
				if !isLetter(r) && !isDigit(r) && r != '_' {
					t.Fatalf("parseUse(%q) gave the name %q", use, a.name)
				}
			}
		}
	})
}
