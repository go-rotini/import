// Package walk holds what every importer shares: valid rotini names, argument names read from
// a usage line, function positions for hook pointers, and the build facts a description's
// source names.
package walk

import (
	"reflect"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"

	imp "github.com/go-rotini/import"
)

const importerModule = "github.com/go-rotini/import"

// IsLetter reports whether r is an ASCII letter.
func IsLetter(r rune) bool { return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') }

// IsDigit reports whether r is an ASCII digit.
func IsDigit(r rune) bool { return r >= '0' && r <= '9' }

// CommandName makes s a valid rotini command or flag name: a letter, then letters, digits,
// '_' and '-'. Characters before the first letter are dropped, other characters become
// '-', and a name with no letter becomes "cmd".
func CommandName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case IsLetter(r):
			b.WriteRune(r)
		case b.Len() == 0:
		case IsDigit(r) || r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "cmd"
	}
	return b.String()
}

// ValidName reports whether s is already a valid rotini command or flag name.
func ValidName(s string) bool { return s != "" && CommandName(s) == s }

// JoinPath appends name to a command path.
func JoinPath(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + " " + name
}

// ArgName turns a placeholder into an argument name: lower case letters, digits and '_',
// starting with a letter.
func ArgName(s string) string {
	var b strings.Builder
	under := false
	for _, r := range strings.ToLower(s) {
		if IsLetter(r) || IsDigit(r) {
			if under && b.Len() > 0 {
				b.WriteByte('_')
			}
			under = false
			b.WriteRune(r)
			continue
		}
		under = true
	}
	out := b.String()
	if out != "" && !IsLetter(rune(out[0])) {
		out = "arg_" + out
	}
	return out
}

// UniqueNames gives repeated argument names a numeric suffix: name, name_2, name_3.
func UniqueNames(args []*imp.Argument) {
	used := map[string]bool{}
	for _, a := range args {
		name := a.Name
		for n := 2; used[name]; n++ {
			name = a.Name + "_" + strconv.Itoa(n)
		}
		a.Name = name
		used[name] = true
	}
}

// SplitExamples splits an example block into its non-empty lines, trimmed.
func SplitExamples(ex string) []string {
	var out []string
	for _, l := range strings.Split(ex, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// FuncValue returns f's code pointer, or 0 for a nil or non-function value.
func FuncValue(f any) uintptr {
	v := reflect.ValueOf(f)
	if !v.IsValid() || v.Kind() != reflect.Func || v.IsNil() {
		return 0
	}
	return v.Pointer()
}

// FuncName returns f's full name as the Go runtime reports it, or "" for a nil or
// non-function value.
func FuncName(f any) string {
	pc := FuncValue(f)
	if pc == 0 {
		return ""
	}
	if fn := runtime.FuncForPC(pc); fn != nil {
		return fn.Name()
	}
	return "?"
}

// FuncPos returns a function's name, file and line. A file in the module cache is given
// relative to it ("example.com/x@v1.2.3/cmd.go").
func FuncPos(f any) (name, file string, line int) {
	pc := FuncValue(f)
	if pc == 0 {
		return "", "", 0
	}
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return "?", "", 0
	}
	file, line = fn.FileLine(pc)
	if _, rest, ok := strings.Cut(file, "/pkg/mod/"); ok {
		file = rest
	}
	return fn.Name(), file, line
}

// SameFunc reports whether a and b are the same function.
func SameFunc(a, b any) bool { return FuncValue(a) == FuncValue(b) }

// Source names the framework, the version of its module in the running program and the
// importer's own version, as far as the build records them.
func Source(framework, module string) imp.Source {
	s := imp.Source{Framework: framework}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return s
	}
	if bi.Main.Path == importerModule {
		s.Importer = bi.Main.Version
	}
	for _, d := range bi.Deps {
		switch d.Path {
		case module:
			s.Version = moduleVersion(d)
		case importerModule:
			s.Importer = moduleVersion(d)
		}
	}
	return s
}

func moduleVersion(m *debug.Module) string {
	if m.Replace != nil && m.Replace.Version != "" {
		return m.Replace.Version
	}
	return m.Version
}

// DependsOn reports whether the running program links the module at path.
func DependsOn(path string) bool {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return false
	}
	for _, d := range bi.Deps {
		if d.Path == path {
			return true
		}
	}
	return false
}
