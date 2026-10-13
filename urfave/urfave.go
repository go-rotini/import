package urfave

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	imp "github.com/go-rotini/import"
	"github.com/go-rotini/import/internal/walk"
	"github.com/urfave/cli/v3"
)

const (
	cliModule = "github.com/urfave/cli/v3"
	cliPrefix = cliModule + "."
)

// Options tunes an import.
type Options struct {
	// Name names the root command when its Name is empty. urfave/cli names such a root after
	// os.Args[0], which is the test binary's name when the import runs under go test; when
	// Name is empty too, that name is used without its ".test" suffix.
	Name string
	// Strict imports a command that declares no Arguments as taking exactly the positionals
	// its ArgsUsage names. By default such a command also gets a trailing variadic "args",
	// because urfave/cli accepts any positionals then.
	Strict bool
}

type importer struct {
	opts Options
	root *cli.Command
	// sep is the separator urfave/cli splits list and map flag values on, "" for none.
	sep   string
	notes []imp.Note
	hooks []imp.Hook
	// inherited maps the imported names of the cascading flags the command being walked
	// inherits to their urfave/cli names; orig holds each imported flag's urfave/cli name.
	inherited map[string]string
	orig      map[*imp.Flag]string
	// facts gathered for the once-per-import notes
	deprecations bool
	noAction     bool
	groupHelp    bool   // a command below the root has sub-commands
	testRoot     string // the root's Name when it is the test binary's name, else ""
}

// FromURFave imports the tree that cmd belongs to, starting at its root. It reads the tree
// as declared and never runs it, so it adds the help and version flags and the help and
// completion commands urfave/cli would add while running, marked as builtin. A panic while
// walking is returned as an error.
func FromURFave(cmd *cli.Command, opts Options) (res *imp.Result, err error) {
	if cmd == nil {
		return nil, errors.New("urfave import: no command to import")
	}
	defer func() {
		if r := recover(); r != nil {
			res, err = nil, fmt.Errorf("urfave import: walking the command tree panicked: %v", r)
		}
	}()
	root := cmd.Root()
	im := &importer{opts: opts, root: root, sep: ",", orig: map[*imp.Flag]string{}}
	name := root.Name
	if name == "" {
		name = programName(opts.Name)
	} else if prog, ok := walk.ProgramName(name); ok {
		im.testRoot, name = name, prog
	}
	im.separator(walk.CommandName(name))
	rc := im.command(root, walk.CommandName(name), "", name)
	im.programNotes(rc)
	return &imp.Result{
		Source:  walk.Source("urfave", cliModule),
		Command: rc,
		Notes:   im.notes,
		Hooks:   im.hooks,
	}, nil
}

// programName is the root's name: the option, else the running binary's name without the
// suffix go test gives it.
func programName(name string) string {
	if name != "" {
		return name
	}
	base := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	return strings.TrimSuffix(base, ".test")
}

// separator reads the root's list separator settings.
func (im *importer) separator(path string) {
	switch sep := im.root.SliceFlagSeparator; {
	case im.root.DisableSliceFlagSeparator:
		im.sep = ""
	case sep == "":
	case len([]rune(sep)) == 1:
		im.sep = sep
	default:
		im.sep = ""
		im.notef(path, imp.Lossy, "SliceFlagSeparator %q dropped: a rotini separator is one character, so list and map flags take one value per occurrence", sep)
	}
}

func (im *importer) notef(path string, level imp.Level, format string, a ...any) {
	im.notes = append(im.notes, imp.Note{Path: path, Level: level, Msg: fmt.Sprintf(format, a...)})
}

// field returns the named field of the struct p points at, or an invalid value when the
// struct has no such field: the urfave/cli release in use predates it.
func field(p any, name string) reflect.Value {
	v := reflect.ValueOf(p)
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return reflect.Value{}
	}
	return v.FieldByName(name)
}

func fieldString(p any, name string) string {
	if f := field(p, name); f.IsValid() && f.Kind() == reflect.String {
		return f.String()
	}
	return ""
}

func fieldBool(p any, name string) bool {
	f := field(p, name)
	return f.IsValid() && f.Kind() == reflect.Bool && f.Bool()
}

// fieldFunc returns the named function field, or nil when it is unset or absent.
func fieldFunc(p any, name string) any {
	if f := field(p, name); f.IsValid() && f.Kind() == reflect.Func && !f.IsNil() {
		return f.Interface()
	}
	return nil
}

func isCLIFunc(f any) bool { return strings.HasPrefix(walk.FuncName(f), cliPrefix) }

func (im *importer) command(c *cli.Command, name, parent, origName string) *imp.Command {
	isRoot := parent == ""
	path := walk.JoinPath(parent, name)
	if walk.CommandName(origName) != origName {
		im.notef(path, imp.Lossy, "command name %q is not a valid rotini name; renamed to %q", origName, name)
	}
	rc := &imp.Command{
		Name:        name,
		Summary:     strings.TrimSpace(c.Usage),
		Description: strings.TrimSpace(c.Description),
		Group:       strings.TrimSpace(c.Category),
		Hidden:      c.Hidden,
		Deprecated:  fieldString(c, "Deprecated"),
	}
	if rc.Description == rc.Summary {
		rc.Description = ""
	}
	if rc.Deprecated != "" {
		im.deprecations = true
	}
	if isRoot {
		if len(c.Aliases) > 0 {
			im.notef(path, imp.Info, "aliases %v on the root dropped: the root is reached by running the binary", c.Aliases)
		}
	} else {
		rc.Aliases = im.aliases(c.Aliases, path)
	}
	im.commandHooks(c, path)
	im.commandNotes(c, path, isRoot)

	subs := realCommands(c)
	rc.Flags, rc.FlagGroups = im.flags(c, path, len(subs) > 0)
	switch stop := stopOnNthArg(c); {
	case c.SkipFlagParsing || stop == 0:
		im.passthrough(path, rc, len(subs) > 0)
	default:
		rc.Arguments = im.arguments(c, path, len(subs) > 0)
		if stop > 0 {
			im.stopOnNthArg(path, rc, stop)
		}
		if isRoot {
			rc.Flags = im.builtinFlags(c, path, rc.Flags)
		}
	}

	if rc.Passthrough {
		return rc
	}
	inherited := im.inherited
	im.inherited = walk.Cascade(inherited, rc.Flags, im.orig)
	defer func() { im.inherited = inherited }()
	names := walk.Names{}
	for _, sub := range subs {
		subName, why := names.Rename(sub.Name)
		if why != "" && subName != walk.CommandName(sub.Name) {
			im.notef(walk.JoinPath(path, subName), imp.Lossy, "command name %q %s; renamed to %q", sub.Name, why, subName)
		}
		rc.Commands = append(rc.Commands, im.command(sub, subName, path, sub.Name))
	}
	if !isRoot && len(subs) > 0 {
		im.groupHelp = true
	}
	if isRoot {
		rc.Commands = append(rc.Commands, im.builtinCommands(c, path)...)
	}
	return rc
}

// realCommands lists c's sub-commands, leaving out the help command urfave/cli adds when the
// tree has run before.
func realCommands(c *cli.Command) []*cli.Command {
	var out []*cli.Command
	for _, sub := range c.Commands {
		if sub.Name == "help" && isCLIFunc(sub.Action) {
			continue
		}
		out = append(out, sub)
	}
	return out
}

func (im *importer) aliases(as []string, path string) []string {
	var out []string
	for _, a := range as {
		if !walk.ValidName(a) {
			im.notef(path, imp.Lossy, "alias %q dropped: not a valid rotini name", a)
			continue
		}
		out = append(out, a)
	}
	return out
}

// commandHooks records where each lifecycle function of c lives. urfave/cli runs the Before
// and After of every command on the path, as rotini runs the cascading hooks.
func (im *importer) commandHooks(c *cli.Command, path string) {
	hooks := []struct {
		framework, rotini string
		fn                any
	}{
		{"Before", "CascadingPreRun", c.Before},
		{"Action", "Run", c.Action},
		{"After", "CascadingPostRun", c.After},
	}
	for _, h := range hooks {
		fn, file, line := walk.FuncPos(h.fn)
		if fn == "" || isCLIFunc(h.fn) {
			continue
		}
		im.hooks = append(im.hooks, imp.Hook{
			Path: path, Framework: h.framework, Rotini: h.rotini, Func: fn, File: file, Line: line,
		})
	}
	if c.Action == nil {
		im.noAction = true
	}
}

func (im *importer) commandNotes(c *cli.Command, path string, isRoot bool) {
	if c.DefaultCommand != "" {
		im.notef(path, imp.Unsupported, "DefaultCommand %q dropped: rotini runs no command unless it is named; have this command's handler call its code", c.DefaultCommand)
	}
	if c.PrefixMatchCommands {
		im.notef(path, imp.Unsupported, "PrefixMatchCommands dropped: rotini matches command names exactly")
	}
	if c.Suggest {
		im.notef(path, imp.Unsupported, "Suggest dropped: rotini makes no suggestions unless the program asks for them")
	}
	if c.ReadArgsFromStdin {
		im.notef(path, imp.Unsupported, "ReadArgsFromStdin dropped: declare stdin: and read it in the handler")
	}
	if c.AllowExtFlags {
		im.notef(path, imp.Unsupported, "AllowExtFlags dropped: flags declared with the standard flag package were not imported")
	}
	if strings.TrimSpace(c.UsageText) != "" {
		im.notef(path, imp.Lossy, "custom UsageText: rotini generates usage from the spec")
	}
	if c.CustomHelpTemplate != "" || (isRoot && c.CustomRootCommandHelpTemplate != "") {
		im.notef(path, imp.Lossy, "custom help template: rotini generates help from the spec; port it as a verbatim help page")
	}
	if f := c.ShellComplete; f != nil && !isCLIFunc(f) {
		im.notef(path, imp.Lossy, "ShellComplete %s: reimplement it as a rotini completer", walk.FuncName(f))
	}
	if f := fieldFunc(c, "ArgValidator"); f != nil {
		im.notef(path, imp.Lossy, "ArgValidator %s: move its checks to the handler", walk.FuncName(f))
	}
	for _, h := range []struct {
		name string
		fn   any
	}{
		{"OnUsageError", c.OnUsageError},
		{"CommandNotFound", c.CommandNotFound},
		{"InvalidFlagAccessHandler", c.InvalidFlagAccessHandler},
		{"ExitErrHandler", c.ExitErrHandler},
	} {
		if walk.FuncValue(h.fn) != 0 {
			im.notef(path, imp.Lossy, "%s %s: rotini reports errors through the program's Reporter", h.name, walk.FuncName(h.fn))
		}
	}
	if f := fieldFunc(c, "ConfigureShellCompletionCommand"); f != nil {
		im.notef(path, imp.Lossy, "ConfigureShellCompletionCommand %s: its changes to the completion command were not imported", walk.FuncName(f))
	}
}

// stopOnNthArg reads StopOnNthArg, which newer urfave/cli releases have: -1 when unset.
func stopOnNthArg(c *cli.Command) int {
	f := field(c, "StopOnNthArg")
	if !f.IsValid() || f.Kind() != reflect.Pointer || f.IsNil() || !f.Elem().CanInt() {
		return -1
	}
	return int(f.Elem().Int())
}

// stopOnNthArg imports StopOnNthArg: flags stop after n positionals, which is options_first
// for one and a passthrough argument for a last variadic argument at that position.
func (im *importer) stopOnNthArg(path string, rc *imp.Command, n int) {
	switch {
	case n == 1 && len(rc.Arguments) > 0:
		rc.OptionsFirst = true
	case n < len(rc.Arguments) && n == len(rc.Arguments)-1 && rc.Arguments[n].Schema.Type == "[]string":
		a := rc.Arguments[n]
		a.Passthrough = true
		a.Schema.Separator = ""
	default:
		im.notef(path, imp.Lossy, "StopOnNthArg %d dropped: no argument starts at that position for the words to pass through to", n)
	}
}

// passthrough imports a command that skips flag parsing as a passthrough command: every
// word after its name reaches its one argument as typed. Its flags and sub-commands are
// dropped, since a passthrough command has neither.
func (im *importer) passthrough(path string, rc *imp.Command, hasSubs bool) {
	rc.Passthrough = true
	rc.Arguments = []*imp.Argument{{
		Name:    "args",
		Summary: "arguments passed through as typed",
		Schema:  &imp.Schema{Type: "[]string"},
	}}
	if len(rc.Flags) > 0 || hasSubs {
		im.notef(path, imp.Unsupported, "SkipFlagParsing with its own flags or sub-commands: a passthrough command has neither, so they were dropped")
	}
	rc.Flags, rc.FlagGroups = nil, nil
}

// programNotes adds the notes that concern the whole import, once each.
func (im *importer) programNotes(rc *imp.Command) {
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
	if im.groupHelp {
		im.notef(path, imp.Info, "urfave/cli also answers \"<command> help\" on each command with sub-commands; rotini's help command is on the root only, so \"help <command>\" or --help reaches that help")
	}
	if im.deprecations {
		im.notef(path, imp.Info, "urfave/cli prints deprecation warnings itself; in rotini a hook reads rotini.Deprecations and prints them")
	}
	if im.noAction {
		im.notef(path, imp.Info, "urfave/cli prints help for a command with no Action; rotini runs that command's handler, so make it print help")
	}
	if walk.DependsOn("github.com/urfave/cli-altsrc/v3") {
		im.notef(path, imp.Lossy, "cli-altsrc is linked in: values read from configuration files were not imported; declare config_files and key: on inputs by hand")
	}
}
