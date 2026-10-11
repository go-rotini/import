package walk

import "strings"

// UseArg is one argument placeholder of a usage line.
type UseArg struct {
	Name     string
	Optional bool
	Variadic bool
}

// ParseUse reads the argument placeholders that follow the command name in a usage line
// such as "deploy <service> [revision]". Flag placeholders ("[flags]", "-f FILE", "--") are
// skipped; each other placeholder gives one argument named after its first word.
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
	for _, f := range fields {
		inner := strings.TrimSpace(strings.ReplaceAll(unwrap(f), "...", " "))
		lead := strings.TrimLeft(inner, "([<")
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
		bare := strings.TrimSuffix(f, "...")
		out = append(out, UseArg{
			Name:     name,
			Optional: strings.HasPrefix(bare, "[") && strings.HasSuffix(bare, "]"),
			Variadic: strings.Contains(f, "..."),
		})
	}
	return out
}

// useFields splits a usage line on spaces, keeping a bracketed group such as
// "[trace directory]" or "(-f FILE | TYPE NAME)" as one field.
func useFields(use string) []string {
	var out []string
	depth, start := 0, -1
	for i, r := range use {
		switch r {
		case '[', '<', '(':
			depth++
		case ']', '>', ')':
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

// unwrap removes one layer of enclosing brackets from a usage field.
func unwrap(f string) string {
	f = strings.TrimSuffix(f, "...")
	if len(f) >= 2 {
		switch {
		case f[0] == '[' && f[len(f)-1] == ']',
			f[0] == '<' && f[len(f)-1] == '>',
			f[0] == '(' && f[len(f)-1] == ')':
			return f[1 : len(f)-1]
		}
	}
	return strings.Trim(f, "[]<>()")
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
