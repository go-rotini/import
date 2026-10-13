package cobra

import (
	"strings"

	imp "github.com/go-rotini/import"
	"github.com/go-rotini/import/internal/walk"
	"github.com/spf13/cobra"
)

const (
	cobraModule    = "github.com/spf13/cobra"
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
	path := walk.JoinPath(parentPath, "completion")
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
		name, file, line := walk.FuncPos(h.fn)
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

// isCobraFunc reports whether f is Cobra's own code: a stock validator or a default. A
// closure such as the one cobra.ExactArgs returns can be named after the function it was
// inlined into, so a function written in Cobra's own source directory counts too.
func isCobraFunc(f any) bool {
	return strings.HasPrefix(walk.FuncName(f), cobraPkgPrefix) || walk.DefinedBeside(f, cobra.NoArgs)
}
