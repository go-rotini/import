package walk

import "strings"

// UseArg is one argument placeholder of a usage line.
type UseArg struct {
	Name     string
	Optional bool
	Variadic bool
	// Choice marks a placeholder made from a required group of alternatives one of which is a
	// flag, such as "(-f FILE | TYPE NAME)": the flag can stand in for it, so it was made
	// optional.
	Choice bool
}

// ParseUse reads the argument placeholders that follow the command name in a usage line
// such as "deploy <service> [revision]". Flag placeholders ("[flags]", "-f FILE", "--") are
// skipped; each other placeholder gives one argument named after its first word. A group of
// alternatives ("{<number> | <url>}") gives one argument named after its first placeholder,
// optional when another alternative is a flag ("(-f FILE | TYPE NAME)"). A lone "..." makes
// the placeholder before it variadic; the one after it closes the run when it is the
// same name with another index ("KEY_1=VAL_1 ... KEY_N=VAL_N").
func ParseUse(use string) []UseArg {
	fields := useFields(use)
	if len(fields) < 2 {
		return nil
	}
	return placeholders(fields[1:])
}

// ParseArgsUsage reads the argument placeholders of a usage line that holds only them, with
// no command name in front: "<src>... <dst>".
func ParseArgsUsage(usage string) []UseArg { return placeholders(useFields(usage)) }

func placeholders(fields []string) []UseArg {
	var out []UseArg
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if f == "..." || f == "…" {
			if len(out) > 0 && !out[len(out)-1].Variadic {
				out[len(out)-1].Variadic = true
				// The placeholder after the ellipsis ends the run the variadic one holds when
				// it is the same name with another index ("KEY_1 ... KEY_N").
				if i+1 < len(fields) && sameRun(out[len(out)-1].Name, ArgName(leadingWord(strings.TrimLeft(unwrap(fields[i+1]), "([<{")))) {
					i++
				}
			}
			continue
		}
		bare := strings.TrimSuffix(f, "...")
		optional := enclosed(bare, '[', ']')
		inner := strings.TrimSpace(strings.ReplaceAll(unwrap(f), "...", " "))
		choice := false
		if alts := alternatives(inner); len(alts) > 1 {
			var orFlag bool
			inner, orFlag = choicePlaceholder(alts)
			if inner == "" {
				continue
			}
			if orFlag {
				choice = !optional
				optional = true
			}
		}
		lead := strings.TrimLeft(inner, "([<{")
		if lead == "" || strings.HasPrefix(lead, "-") || strings.HasPrefix(lead, "|") {
			continue
		}
		switch strings.ToLower(strings.Fields(lead)[0]) {
		case "flags", "options", "global":
			continue
		}
		name := ArgName(leadingWord(lead))
		if name == "" {
			continue
		}
		out = append(out, UseArg{
			Name:     name,
			Optional: optional,
			Variadic: strings.Contains(f, "..."),
			Choice:   choice,
		})
	}
	return out
}

// alternatives splits s on the '|' that are outside brackets.
func alternatives(s string) []string {
	var out []string
	depth, start := 0, 0
	for i, r := range s {
		switch r {
		case '[', '<', '(', '{':
			depth++
		case ']', '>', ')', '}':
			if depth > 0 {
				depth--
			}
		case '|':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	return append(out, strings.TrimSpace(s[start:]))
}

// choicePlaceholder returns the first alternative that is a positional placeholder, or ""
// when there is none, and whether another alternative is a flag, which leaves the positional
// out. Plain lower-case words beside a flag ("(-o|--output=)json|yaml") are the flag's
// values, not placeholders.
func choicePlaceholder(alts []string) (placeholder string, orFlag bool) {
	var positional []string
	flag := false
	for _, a := range alts {
		if lead := strings.TrimLeft(a, "([<{"); lead == "" || strings.HasPrefix(lead, "-") {
			flag = flag || lead != ""
			continue
		}
		positional = append(positional, a)
	}
	if len(positional) == 0 {
		return "", flag
	}
	if flag {
		literal := true
		for _, p := range positional {
			if p != strings.ToLower(p) || strings.ContainsAny(p, "<[({") {
				literal = false
				break
			}
		}
		if literal {
			return "", flag
		}
	}
	return positional[0], flag
}

// useFields splits a usage line on spaces, keeping a bracketed group such as
// "[trace directory]", "{<number> | <url>}" or "(-f FILE | TYPE NAME)" as one field.
func useFields(use string) []string {
	var out []string
	depth, start := 0, -1
	for i, r := range use {
		switch r {
		case '[', '<', '(', '{':
			depth++
		case ']', '>', ')', '}':
			if depth > 0 {
				depth--
			}
		}
		if (r == ' ' || r == '\t' || r == '\n') && depth == 0 {
			if start >= 0 {
				out = append(out, use[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, use[start:])
	}
	return out
}

func enclosed(f string, first, last byte) bool {
	return len(f) >= 2 && f[0] == first && f[len(f)-1] == last
}

// unwrap removes one layer of enclosing brackets from a usage field.
func unwrap(f string) string {
	f = strings.TrimSuffix(f, "...")
	if enclosed(f, '[', ']') || enclosed(f, '<', '>') || enclosed(f, '(', ')') || enclosed(f, '{', '}') {
		return f[1 : len(f)-1]
	}
	return strings.Trim(f, "[]<>(){}")
}

// sameRun reports whether two placeholder names are one name with different indexes, or
// with an index and without one: "key_1" and "key_n", "file1" and "filen", "file" and "filen".
func sameRun(a, b string) bool {
	if a == b {
		return true
	}
	sa, ia := cutIndex(a)
	sb, ib := cutIndex(b)
	return (ia && ib && sa != "" && sa == sb) || (ib && a == sb) || (ia && b == sa)
}

// cutIndex removes a trailing index (digits, or "n") and the '_' before it from a name.
func cutIndex(s string) (stem string, ok bool) {
	stem = strings.TrimRight(s, "0123456789")
	if stem == s {
		stem, ok = strings.CutSuffix(s, "n")
		if !ok {
			return s, false
		}
	}
	return strings.TrimSuffix(stem, "_"), true
}

// leadingWord returns the first run of letters, digits, '_' and '-' that starts with a
// letter or digit: "TYPE[.VERSION]" gives "TYPE", "<src>" gives "src".
func leadingWord(s string) string {
	start := -1
	for i, r := range s {
		ok := IsLetter(r) || IsDigit(r) || (start >= 0 && (r == '_' || r == '-'))
		switch {
		case ok && start < 0:
			start = i
		case !ok && start >= 0:
			return s[start:i]
		}
	}
	if start < 0 {
		return ""
	}
	return s[start:]
}

// UseNames lists usage placeholders as a note names them: "<a>, <b>".
func UseNames(us []UseArg) string {
	ns := make([]string, 0, len(us))
	for _, u := range us {
		ns = append(ns, "<"+u.Name+">")
	}
	return strings.Join(ns, ", ")
}
