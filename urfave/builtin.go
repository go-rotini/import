package urfave

import (
	"strings"

	imp "github.com/go-rotini/import"
	"github.com/go-rotini/import/internal/walk"
	"github.com/urfave/cli/v3"
)

// completionShells are the shells urfave/cli's completion command writes scripts for, by
// rotini's names: urfave/cli calls PowerShell "pwsh".
var completionShells = []string{"bash", "zsh", "fish", "powershell"}

// builtinFlags adds the help and version flags urfave/cli gives the root while running:
// the help flag first, cascading, as it is on every command, and the version flag last,
// on the root only. An alias a flag of the root already declares is left out, as
// urfave/cli leaves it out, and a flag whose name is taken is not added.
func (im *importer) builtinFlags(c *cli.Command, path string, flags []*imp.Flag) []*imp.Flag {
	taken := map[string]bool{}
	for _, f := range allFlags(c) {
		for _, n := range f.Names() {
			taken[n] = true
		}
	}
	if !c.HideHelp && cli.HelpFlag != nil {
		if f := im.builtinFlag(cli.HelpFlag, "help", taken, path); f != nil {
			f.Cascading = true
			flags = append([]*imp.Flag{f}, flags...)
		}
	}
	if c.Version != "" && !c.HideVersion && cli.VersionFlag != nil {
		if f := im.builtinFlag(cli.VersionFlag, "version", taken, path); f != nil {
			flags = append(flags, f)
			im.notef(path, imp.Info, "Version %q: urfave/cli's version flag is kept as %s; the version string belongs in the build (ldflags) or the root hook",
				c.Version, strings.Join(f.Identifiers, "/"))
		}
	}
	return flags
}

func (im *importer) builtinFlag(f cli.Flag, kind string, taken map[string]bool, path string) *imp.Flag {
	names := f.Names()
	if len(names) == 0 || taken[names[0]] {
		return nil
	}
	var kept []string
	for _, n := range names {
		if n == names[0] || !taken[n] {
			kept = append(kept, n)
		}
	}
	rf := &imp.Flag{
		Name:         kind,
		Identifiers:  im.identifiers(kept, path, names[0]),
		ShortCircuit: true,
		Schema:       &imp.Schema{Type: "bool"},
		Builtin:      kind,
	}
	if u, ok := f.(usageFlag); ok {
		rf.Summary = strings.TrimSpace(u.GetUsage())
	}
	return rf
}

// builtinCommands adds the help and completion commands urfave/cli gives the root while
// running, unless hidden or already declared.
func (im *importer) builtinCommands(c *cli.Command, path string) []*imp.Command {
	var out []*imp.Command
	if !c.HideHelp && !c.HideHelpCommand && c.Command("help") == nil {
		out = append(out, &imp.Command{
			Name:    "help",
			Aliases: []string{"h"},
			Summary: "Shows a list of commands or help for one command",
			Arguments: []*imp.Argument{{
				Name:   "command",
				Schema: &imp.Schema{Type: "[]string"},
			}},
			Builtin: "help",
		})
	}
	if c.EnableShellCompletion || fieldFunc(c, "ConfigureShellCompletionCommand") != nil {
		name := c.ShellCompletionCommandName
		if name == "" {
			name = "completion"
		}
		name = walk.CommandName(name)
		shells := make([]imp.EnumValue, 0, len(completionShells))
		for _, sh := range completionShells {
			shells = append(shells, imp.EnumValue{Value: sh})
		}
		out = append(out, &imp.Command{
			Name:    name,
			Summary: "Output shell completion script for bash, zsh, fish, or Powershell",
			Hidden:  true,
			Arguments: []*imp.Argument{{
				Name:   "shell",
				Schema: &imp.Schema{Type: "string", Required: true, Enum: shells},
			}},
			Builtin: "completion",
		})
		im.notef(walk.JoinPath(path, name), imp.Lossy, "urfave/cli's completion command is imported in the shape rotini seeds, one command with a shell argument, where urfave/cli's pwsh is powershell")
	}
	return out
}
