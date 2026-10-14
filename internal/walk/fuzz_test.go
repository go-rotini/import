package walk

import (
	"strings"
	"testing"
)

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

// FuzzSplitExamples checks that every non-empty line of an example block ends up in exactly
// one example or comment, and that each example, split again on its own, is one example.
func FuzzSplitExamples(f *testing.F) {
	for _, s := range []string{
		"  # create one\n  app create \\\n    --name x\n",
		"app api graphql -f query='\n  query { viewer }\n'",
		"app say don't\napp ls\napp say won't",
		"app x --m=\"a\nb\"\n# it's\napp 'open",
		"\\\n'\n\"\n#",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, block string) {
		lines := 0
		for _, l := range strings.Split(block, "\n") {
			if strings.TrimSpace(l) != "" {
				lines++
			}
		}
		examples, comments := SplitExamples(block)
		got := len(comments)
		for _, e := range examples {
			got += strings.Count(e, "\n") + 1
		}
		if got != lines {
			t.Fatalf("SplitExamples(%q) kept %d of %d lines: %q %q", block, got, lines, examples, comments)
		}
		for _, e := range examples {
			if again, _ := SplitExamples(e); len(again) != 1 || again[0] != e {
				t.Fatalf("SplitExamples(%q) gave the example %q, which splits again into %q", block, e, again)
			}
		}
	})
}

// FuzzMachineText checks that help text keeps no test binary name, directory or host name of
// this machine once scrubbed.
func FuzzMachineText(f *testing.F) {
	for _, s := range []string{
		"Run `eval \"$(app.test completion bash)\"`; app.tests stays.",
		"config (default /home/me/.app.yaml) on box-1",
		"app.testapp.test /home/me/home/me box-1.box-1",
	} {
		f.Add(s)
	}
	m := &Machine{
		dirs:     []machineDir{{"/home/me/.cache", "the user cache directory", "<cache>"}, {"/home/me", "the home directory", "~"}},
		hosts:    []string{"box-1"},
		testName: "app.test",
		name:     "app",
	}
	f.Fuzz(func(t *testing.T, s string) {
		out, _ := m.text(s)
		out, _ = m.tokens(out)
		if w := m.find(out); w != "" {
			t.Fatalf("scrubbed %q to %q, which still holds %s", s, out, w)
		}
	})
}
