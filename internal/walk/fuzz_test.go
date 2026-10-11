package walk

import "testing"

// FuzzParseUse checks that any usage line gives valid argument names, with or without a
// command name in front.
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
		for _, a := range append(ParseUse(use), ParseArgsUsage(use)...) {
			if a.Name == "" || !IsLetter(rune(a.Name[0])) {
				t.Fatalf("ParseUse(%q) gave the name %q", use, a.Name)
			}
			for _, r := range a.Name {
				if !IsLetter(r) && !IsDigit(r) && r != '_' {
					t.Fatalf("ParseUse(%q) gave the name %q", use, a.Name)
				}
			}
		}
	})
}
