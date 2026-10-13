package cobra

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	imp "github.com/go-rotini/import"
	"github.com/go-rotini/import/internal/walk"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Options tunes an import.
type Options struct {
	// Strict imports a command whose Args is unset as taking exactly the positionals its Use
	// line names. By default such a command also gets a trailing variadic "args", because
	// Cobra accepts any positionals when Args is unset.
	Strict bool
}

type importer struct {
	opts        Options
	notes       []imp.Note
	hooks       []imp.Hook
	groupsNoted map[groupKey]bool
	rootName    string // the root's imported name
	testRoot    string // the root's name when it is the test binary's name, else ""
	// inherited maps the imported names of the cascading flags the command being walked
	// inherits to their pflag names; orig holds each imported flag's pflag name.
	inherited map[string]string
	orig      map[*imp.Flag]string
	// facts gathered for the once-per-import notes
	deprecations     bool
	groupNoRun       bool
	nestedPersistent bool
}

// FromCobra imports the tree that cmd belongs to, starting at its root.
//
// It changes the tree the way Cobra's own Execute would: it adds the default help and
// completion commands to the root, the help flag to every command and the version flag to
// every command that sets Version, each where Cobra itself would add it. Call it
// on a tree that is not executing. A panic inside Cobra while walking is returned as an
// error.
func FromCobra(cmd *cobra.Command, opts Options) (res *imp.Result, err error) {
	if cmd == nil {
		return nil, errors.New("cobra import: no command to import")
	}
	defer func() {
		if r := recover(); r != nil {
			res, err = nil, fmt.Errorf("cobra import: walking the command tree panicked: %v", r)
		}
	}()
	root := cmd.Root()
	im := &importer{opts: opts, groupsNoted: map[groupKey]bool{}, orig: map[*imp.Flag]string{}}
	progName, fromTest := walk.ProgramName(root.Name())
	rootName := walk.CommandName(progName)
	im.rootName = rootName
	if fromTest {
		im.testRoot = root.Name()
	}
	if rootName != progName {
		im.notef(rootName, imp.Lossy, "command name %q is not a valid rotini name; renamed to %q", progName, rootName)
	}
	im.addDefaults(root)
	rc := im.command(root, rootName, "")
	im.programNotes(root, rc)
	return &imp.Result{
		Source:  walk.Source("cobra", cobraModule),
		Command: rc,
		Notes:   im.notes,
		Hooks:   im.hooks,
	}, nil
}

// addDefaults asks Cobra to add what it otherwise adds lazily inside Execute, in the same
// order, so the walk sees the tree a user sees. Where Cobra panics adding one, the default
// is left out and noted.
func (im *importer) addDefaults(root *cobra.Command) {
	im.safely(root, "help command", root.InitDefaultHelpCmd)
	im.safely(root, "completion command", func() { root.InitDefaultCompletionCmd() })
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		im.safely(c, "help flag", c.InitDefaultHelpFlag)
		im.safely(c, "version flag", c.InitDefaultVersionFlag)
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
}

func (im *importer) safely(c *cobra.Command, what string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			im.notef(im.commandPath(c), imp.Unsupported, "Cobra could not add its default %s: %v", what, r)
		}
	}()
	fn()
}

// commandPath is c's path as the import names it.
func (im *importer) commandPath(c *cobra.Command) string {
	if !c.HasParent() {
		return im.rootName
	}
	return walk.JoinPath(im.commandPath(c.Parent()), walk.CommandName(c.Name()))
}

func (im *importer) notef(path string, level imp.Level, format string, a ...any) {
	im.notes = append(im.notes, imp.Note{Path: path, Level: level, Msg: fmt.Sprintf(format, a...)})
}

func (im *importer) command(c *cobra.Command, name, parent string) *imp.Command {
	isRoot := !c.HasParent()
	path := walk.JoinPath(parent, name)
	rc := &imp.Command{
		Name:        name,
		Summary:     strings.TrimSpace(c.Short),
		Description: strings.TrimSpace(c.Long),
		Hidden:      c.Hidden,
		Deprecated:  c.Deprecated,
	}
	if rc.Description == rc.Summary {
		rc.Description = ""
	}
	im.examples(rc, path, c.Example)
	if rc.Deprecated != "" {
		im.deprecations = true
	}
	if isRoot {
		rc.DisplayName = c.Annotations[annDisplayName]
		if len(c.Aliases) > 0 {
			im.notef(path, imp.Info, "aliases %v on the root dropped: the root is reached by running the binary", c.Aliases)
		}
	} else {
		rc.Aliases = im.aliases(c, path)
	}
	rc.Group = groupTitle(c)
	im.commandHooks(c, path)
	im.commandNotes(c, path)

	if c.DisableFlagParsing {
		im.passthrough(c, path, rc)
		return rc
	}

	rc.Arguments = im.arguments(c, path)
	im.argCompletion(c, path, rc.Arguments)
	rc.Flags, rc.FlagGroups = im.flags(c, path)
	if !interspersed(c.Flags()) {
		im.optionsFirst(path, rc)
	}
	if c.HasSubCommands() && !c.Runnable() {
		im.groupNoRun = true
	}

	inherited := im.inherited
	im.inherited = walk.Cascade(inherited, rc.Flags, im.orig)
	defer func() { im.inherited = inherited }()
	names := walk.Names{}
	for _, sub := range c.Commands() {
		switch {
		case isDefaultHelpCmd(sub):
			names["help"] = true
		case isDefaultCompletionCmd(sub):
			names["completion"] = true
		}
	}
	for _, sub := range c.Commands() {
		switch {
		case strings.HasPrefix(sub.Name(), "__complete"):
			continue
		case isDefaultHelpCmd(sub):
			rc.Commands = append(rc.Commands, im.helpCommand(sub))
		case isDefaultCompletionCmd(sub):
			rc.Commands = append(rc.Commands, im.completionCommand(c, sub, path))
		default:
			subName := im.subName(path, sub.Name(), names)
			rc.Commands = append(rc.Commands, im.command(sub, subName, path))
		}
	}
	return rc
}

// subName names a sub-command: its own name made valid, with a numeric suffix when that
// is another sub-command's.
func (im *importer) subName(parent, orig string, names walk.Names) string {
	name, why := names.Rename(orig)
	if why != "" {
		im.notef(walk.JoinPath(parent, name), imp.Lossy, "command name %q %s; renamed to %q", orig, why, name)
	}
	return name
}

// examples sets the command's examples from its Example block, folding the block's comment
// lines into one note.
func (im *importer) examples(rc *imp.Command, path, block string) {
	var comments []string
	rc.Examples, comments = walk.SplitExamples(block)
	if len(comments) > 0 {
		im.notef(path, imp.Lossy, "comment lines in the examples dropped, since a rotini example is a command line only: %q", comments)
	}
}

func (im *importer) aliases(c *cobra.Command, path string) []string {
	var out []string
	for _, a := range c.Aliases {
		if !walk.ValidName(a) {
			im.notef(path, imp.Lossy, "alias %q dropped: not a valid rotini name", a)
			continue
		}
		out = append(out, a)
	}
	return out
}

func groupTitle(c *cobra.Command) string {
	if c.GroupID == "" || !c.HasParent() {
		return ""
	}
	for _, g := range c.Parent().Groups() {
		if g.ID == c.GroupID {
			return strings.TrimSuffix(strings.TrimSpace(g.Title), ":")
		}
	}
	return c.GroupID
}

func (im *importer) commandNotes(c *cobra.Command, path string) {
	if len(c.SuggestFor) > 0 {
		im.notef(path, imp.Unsupported, "SuggestFor %v dropped: rotini makes no suggestions unless the program asks for them", c.SuggestFor)
	}
	if c.TraverseChildren {
		im.notef(path, imp.Info, "TraverseChildren: rotini already parses an ancestor's flags before a sub-command name")
	}
	if c.FParseErrWhitelist.UnknownFlags {
		im.notef(path, imp.Unsupported, "FParseErrWhitelist.UnknownFlags dropped: rotini rejects unknown flags (use a passthrough command)")
	}
	if c.HasParent() && c.IsAdditionalHelpTopicCommand() {
		im.notef(path, imp.Info, "help topic imported as a command; its text is the description")
	}
	im.templateNotes(c, path)
}

// templateNotes reports custom help and usage rendering where it is set, not on every
// command that inherits it.
func (im *importer) templateNotes(c *cobra.Command, path string) {
	var parent *cobra.Command
	if c.HasParent() {
		parent = c.Parent()
	}
	if f := c.HelpFunc(); !isCobraFunc(f) && (parent == nil || !walk.SameFunc(f, parent.HelpFunc())) {
		im.notef(path, imp.Lossy, "custom help function %s: rotini generates help from the spec; port it as a verbatim help page or a help handler", walk.FuncName(f))
	}
	if f := c.UsageFunc(); !isCobraFunc(f) && (parent == nil || !walk.SameFunc(f, parent.UsageFunc())) {
		im.notef(path, imp.Lossy, "custom usage function %s: rotini generates usage from the spec", walk.FuncName(f))
	}
	def := &cobra.Command{}
	for _, t := range []struct{ name, got, def, inherited string }{
		{"help template", c.HelpTemplate(), def.HelpTemplate(), parentString(parent, (*cobra.Command).HelpTemplate)},
		{"usage template", c.UsageTemplate(), def.UsageTemplate(), parentString(parent, (*cobra.Command).UsageTemplate)},
	} {
		if t.got != t.def && (parent == nil || t.got != t.inherited) {
			im.notef(path, imp.Lossy, "custom %s: rotini generates help from the spec; port it as a verbatim help page", t.name)
		}
	}
	if c.Version != "" && parent == nil && c.VersionTemplate() != def.VersionTemplate() {
		im.notef(path, imp.Lossy, "custom version template: print the version from the root's hook or the version handler")
	}
}

func parentString(p *cobra.Command, f func(*cobra.Command) string) string {
	if p == nil {
		return ""
	}
	return f(p)
}

// passthrough imports a command that disables flag parsing as a passthrough command: every
// word after its name reaches its one argument as typed.
func (im *importer) passthrough(c *cobra.Command, path string, rc *imp.Command) {
	rc.Passthrough = true
	rc.Arguments = []*imp.Argument{{
		Name:    "args",
		Summary: "arguments passed through as typed",
		Schema:  &imp.Schema{Type: "[]string"},
	}}
	own := 0
	c.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if !setByCobra(f) {
			own++
		}
	})
	if own > 0 || c.HasSubCommands() {
		im.notef(path, imp.Unsupported, "DisableFlagParsing with its own flags or sub-commands: a passthrough command has neither, so they were dropped")
	}
}

// interspersed reports whether flags may follow a positional in fs, the flag set Cobra parses
// a command's words with. pflag's SetInterspersed has no getter, so the unexported field is
// read; a pflag without it is taken as interspersed, its default.
func interspersed(fs *pflag.FlagSet) bool {
	v := reflect.ValueOf(fs).Elem().FieldByName("interspersed")
	return !v.IsValid() || v.Kind() != reflect.Bool || v.Bool()
}

// optionsFirst imports SetInterspersed(false): flags stop at the command's first argument.
func (im *importer) optionsFirst(path string, rc *imp.Command) {
	if len(rc.Arguments) == 0 {
		im.notef(path, imp.Info, "SetInterspersed(false) dropped: the command takes no arguments, so there is no first argument for flags to stop at")
		return
	}
	rc.OptionsFirst = true
}

// programNotes adds the notes that concern the whole import, once each.
func (im *importer) programNotes(root *cobra.Command, rc *imp.Command) {
	path := rc.Name
	m := walk.NewMachine(path)
	m.ScrubNotes(im.notes)
	m.Scrub(rc, path, func(p string, level imp.Level, msg string) { im.notef(p, level, "%s", msg) })
	if im.testRoot != "" {
		im.notef(path, imp.Info, "the root's name %q is the test binary's name, which the program read from os.Args[0]; it was imported as %q", im.testRoot, path)
	}
	for _, msg := range walk.SharedHooks(im.hooks) {
		im.notef(path, imp.Info, "%s", msg)
	}
	if im.deprecations {
		im.notef(path, imp.Info, "Cobra prints deprecation warnings itself; in rotini a hook reads rotini.Deprecations and prints them")
	}
	if im.groupNoRun {
		im.notef(path, imp.Info, "Cobra prints help for a command with sub-commands and no Run; rotini runs that command's handler, so make it print help")
	}
	if im.nestedPersistent {
		im.notef(path, imp.Info, "Cobra runs only the nearest PersistentPreRun and PersistentPostRun; rotini runs every ancestor's CascadingPreRun and CascadingPostRun")
	}
	if cobra.EnableCommandSorting {
		im.notef(path, imp.Info, "commands are imported in Cobra's alphabetical order; rotini lists them in the order the spec declares them")
	}
	if len(im.hooks) > 0 {
		im.notef(path, imp.Info, "flags and commands added inside a hook (PersistentPreRun, PreRun, Run), an OnInitialize function or a plugin loader are not in the walked tree and were not imported")
	}
	if !hasFlagCompletionLookup(root) {
		im.notef(path, imp.Info, "this Cobra version does not expose flag completion functions; any registered ones were not detected")
	}
	if walk.DependsOn("github.com/spf13/viper") {
		im.notef(path, imp.Lossy, "viper is linked in: its bindings (BindPFlag, AutomaticEnv, SetEnvPrefix, config paths) were not imported; declare variable: and key: on inputs, env_prefix and config_files by hand")
	}
}
