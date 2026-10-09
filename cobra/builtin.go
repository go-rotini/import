package cobra

import (
	"reflect"
	"runtime"
	"runtime/debug"
	"strings"

	imp "github.com/go-rotini/import"
	"github.com/spf13/cobra"
)

const (
	cobraModule    = "github.com/spf13/cobra"
	importerModule = "github.com/go-rotini/import/cobra"
	cobraPkgPrefix = cobraModule + "."
)

// isDefaultHelpCmd reports whether sub is the help command Cobra adds.
func isDefaultHelpCmd(sub *cobra.Command) bool {
	return sub.Name() == "help" && isCobraFunc(sub.Run)
}

// isDefaultCompletionCmd reports whether sub is the completion command Cobra adds, whose
// sub-commands (bash, zsh, fish, powershell) are all Cobra's.
func isDefaultCompletionCmd(sub *cobra.Command) bool {
	if sub.Name() != "completion" || !sub.HasSubCommands() {
		return false
	}
	for _, sh := range sub.Commands() {
		if !isCobraFunc(sh.RunE) && !isCobraFunc(sh.Run) {
			return false
		}
	}
	return true
}

func (im *importer) helpCommand(sub *cobra.Command) *imp.Command {
	return &imp.Command{
		Name:    "help",
		Summary: strings.TrimSpace(sub.Short),
		Hidden:  sub.Hidden,
		Group:   groupTitle(sub),
		Arguments: []*imp.Argument{{
			Name:   "command",
			Schema: &imp.Schema{Type: "[]string"},
		}},
		Builtin: "help",
	}
}

// completionCommand merges Cobra's completion command and its per-shell sub-commands into
// one command that takes the shell as an argument.
func (im *importer) completionCommand(parent, sub *cobra.Command, parentPath string) *imp.Command {
	path := joinPath(parentPath, "completion")
	var shells []imp.EnumValue
	noDesc := false
	for _, sh := range sub.Commands() {
		if strings.HasPrefix(sh.Name(), "__") || isDefaultHelpCmd(sh) {
			continue
		}
		shells = append(shells, imp.EnumValue{Value: sh.Name()})
		if f := sh.Flags().Lookup("no-descriptions"); f != nil {
			noDesc = true
		}
	}
	rc := &imp.Command{
		Name:    "completion",
		Summary: strings.TrimSpace(sub.Short),
		Hidden:  sub.Hidden,
		Group:   groupTitle(sub),
		Arguments: []*imp.Argument{{
			Name:   "shell",
			Schema: &imp.Schema{Type: "string", Required: true, Enum: shells},
		}},
		Builtin: "completion",
	}
	if noDesc {
		rc.Flags = []*imp.Flag{{
			Name:        "no-descriptions",
			Summary:     "disable completion descriptions",
			Identifiers: []string{"--no-descriptions"},
			Schema:      &imp.Schema{Type: "bool"},
		}}
	}
	im.notef(path, imp.Info, "Cobra's completion sub-commands merged into one command with a shell argument")
	if parent.CompletionOptions.DisableDescriptions {
		im.notef(path, imp.Info, "completion descriptions are turned off in Cobra (CompletionOptions.DisableDescriptions)")
	}
	return rc
}

// commandHooks records where each lifecycle function of c lives.
func (im *importer) commandHooks(c *cobra.Command, path string) {
	hooks := []struct {
		framework, rotini string
		fn                any
	}{
		{"PersistentPreRun", "CascadingPreRun", c.PersistentPreRun},
		{"PersistentPreRunE", "CascadingPreRun", c.PersistentPreRunE},
		{"PreRun", "PreRun", c.PreRun},
		{"PreRunE", "PreRun", c.PreRunE},
		{"Run", "Run", c.Run},
		{"RunE", "Run", c.RunE},
		{"PostRun", "PostRun", c.PostRun},
		{"PostRunE", "PostRun", c.PostRunE},
		{"PersistentPostRun", "CascadingPostRun", c.PersistentPostRun},
		{"PersistentPostRunE", "CascadingPostRun", c.PersistentPostRunE},
	}
	for _, h := range hooks {
		name, file, line := funcPos(h.fn)
		if name == "" {
			continue
		}
		im.hooks = append(im.hooks, imp.Hook{
			Path: path, Framework: h.framework, Rotini: h.rotini, Func: name, File: file, Line: line,
		})
	}
	if hasPersistentHook(c) {
		for p := c.Parent(); p != nil; p = p.Parent() {
			if hasPersistentHook(p) {
				im.nestedPersistent = true
				break
			}
		}
	}
}

func hasPersistentHook(c *cobra.Command) bool {
	return c.PersistentPreRun != nil || c.PersistentPreRunE != nil ||
		c.PersistentPostRun != nil || c.PersistentPostRunE != nil
}

// funcValue returns f's code pointer, or 0 for a nil or non-function value.
func funcValue(f any) uintptr {
	v := reflect.ValueOf(f)
	if !v.IsValid() || v.Kind() != reflect.Func || v.IsNil() {
		return 0
	}
	return v.Pointer()
}

func funcName(f any) string {
	pc := funcValue(f)
	if pc == 0 {
		return ""
	}
	if fn := runtime.FuncForPC(pc); fn != nil {
		return fn.Name()
	}
	return "?"
}

// funcPos returns a function's name, file and line. A file in the module cache is given
// relative to it ("example.com/x@v1.2.3/cmd.go").
func funcPos(f any) (name, file string, line int) {
	pc := funcValue(f)
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

// isCobraFunc reports whether f is Cobra's own code: a stock validator or a default.
func isCobraFunc(f any) bool {
	return strings.HasPrefix(funcName(f), cobraPkgPrefix)
}

func sameFunc(a, b any) bool { return funcValue(a) == funcValue(b) }

func splitExamples(ex string) []string {
	var out []string
	for _, l := range strings.Split(ex, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// commandName makes s a valid rotini command or flag name: a letter, then letters, digits,
// '_' and '-'. Other characters become '-'.
func commandName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case isLetter(r):
			b.WriteRune(r)
		case b.Len() == 0:
		case isDigit(r) || r == '_' || r == '-':
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

func validName(s string) bool { return s != "" && commandName(s) == s }

func source() imp.Source {
	s := imp.Source{Framework: "cobra"}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return s
	}
	if bi.Main.Path == importerModule {
		s.Importer = bi.Main.Version
	}
	for _, d := range bi.Deps {
		switch d.Path {
		case cobraModule:
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

// dependsOn reports whether the running program links the module at path.
func dependsOn(path string) bool {
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
