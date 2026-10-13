package kong

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/alecthomas/kong"
	imp "github.com/go-rotini/import"
	"github.com/go-rotini/import/internal/walk"
)

const (
	kongModule = "github.com/alecthomas/kong"
	kongPkg    = "github.com/alecthomas/kong"
)

// Options tunes an import.
type Options struct {
	// Name is the program's name. Kong names the root after os.Args[0], which is the test
	// binary's name when the import runs under go test; when Name is empty, that name is used
	// without its ".test" suffix.
	Name string
	// Vars supplies the ${name} interpolations the program passes as kong.Vars in main.
	// A variable left out is replaced by a placeholder and reported.
	Vars map[string]string
	// KongOptions are further options for kong.New, for those the program passes in main
	// that shape the model, such as kong.Description or kong.ExplicitGroups.
	KongOptions []kong.Option
}

type importer struct {
	opts  Options
	app   *kong.Kong
	notes []imp.Note
	hooks []imp.Hook
	// missing names the variables that were replaced by placeholders, sorted.
	missing []string
	// groupNodes counts the nodes that declare flags in each xor group and "and" group.
	groupNodes map[string]int
	// inherited maps the imported names of the cascading flags the node being walked
	// inherits to their Kong names; orig holds each imported flag's Kong name.
	inherited map[string]string
	orig      map[*imp.Flag]string
	// facts gathered for the once-per-import notes
	branchNoRun bool
}

// FromKong imports the command line that grammar, a pointer to a Kong grammar struct,
// declares. It builds the Kong model with kong.New, as the program's own kong.Parse does,
// but parses nothing and runs nothing. A panic inside Kong while building or walking is
// returned as an error.
func FromKong(grammar any, opts Options) (res *imp.Result, err error) {
	if grammar == nil {
		return nil, errors.New("kong import: no grammar to import")
	}
	defer func() {
		if r := recover(); r != nil {
			res, err = nil, fmt.Errorf("kong import: building the Kong model panicked: %v", r)
		}
	}()
	im := &importer{opts: opts, groupNodes: map[string]int{}, orig: map[*imp.Flag]string{}}
	name := walk.CommandName(programName(opts.Name))
	app, err := im.build(grammar, name)
	if err != nil {
		return nil, fmt.Errorf("kong import: %w", err)
	}
	im.app = app
	im.countGroups(app.Model.Node)
	rc := im.command(app.Model.Node, name, "")
	m := walk.NewMachine(name)
	m.ScrubNotes(im.notes)
	m.Scrub(rc, name, func(p string, level imp.Level, msg string) { im.notef(p, level, "%s", msg) })
	im.programNotes(name)
	return &imp.Result{
		Source:  walk.Source("kong", kongModule, kong.New),
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

var undefinedVar = regexp.MustCompile(`undefined variable \$\{([^}]+)\}`)

// placeholder stands in for a variable the caller did not supply. It holds no '$', so Kong
// does not interpolate it again, and marks the text it ends up in.
func placeholder(name string) string { return "\uE000" + name + "\uE001" }

var placeholders = regexp.MustCompile(`\x{E000}([[:alpha:]_]\w*)\x{E001}`)

// build calls kong.New, supplying a placeholder for each variable Kong reports undefined.
func (im *importer) build(grammar any, name string) (*kong.Kong, error) {
	vars := kong.Vars{}
	for k, v := range im.opts.Vars {
		vars[k] = v
	}
	for {
		opts := append([]kong.Option{kong.Name(name), kong.Exit(func(int) {}), vars}, im.opts.KongOptions...)
		app, err := kong.New(grammar, opts...)
		if err == nil {
			sort.Strings(im.missing)
			return app, nil
		}
		m := undefinedVar.FindStringSubmatch(err.Error())
		if m == nil {
			return nil, fmt.Errorf("build the Kong model: %w", err)
		}
		if _, ok := vars[m[1]]; ok {
			return nil, fmt.Errorf("build the Kong model: %w", err)
		}
		vars[m[1]] = placeholder(m[1])
		im.missing = append(im.missing, m[1])
	}
}

// text restores a placeholder in display text to the ${name} it stands for.
func text(s string) string { return placeholders.ReplaceAllString(strings.TrimSpace(s), "$${$1}") }

func hasPlaceholder(s string) bool { return strings.ContainsRune(s, '\uE000') }

func (im *importer) notef(path string, level imp.Level, format string, a ...any) {
	im.notes = append(im.notes, imp.Note{Path: path, Level: level, Msg: fmt.Sprintf(format, a...)})
}

func (im *importer) command(n *kong.Node, name, parent string) *imp.Command {
	path := walk.JoinPath(parent, name)
	rc := &imp.Command{Name: name, Hidden: n.Hidden}
	rc.Summary, rc.Description = helpText(n.Help, n.Detail)
	if n.Group != nil {
		rc.Group = text(n.Group.Title)
	}
	if n.Type == kong.ApplicationNode {
		if len(n.Aliases) > 0 {
			im.notef(path, imp.Info, "aliases %v on the root dropped: the root is reached by running the binary", n.Aliases)
		}
	} else {
		rc.Aliases = im.aliases(n.Aliases, path)
	}
	if n.Type == kong.ArgumentNode {
		im.notef(path, imp.Unsupported, "the branching argument <%s> was imported as a command named %s: rotini takes sub-command names as words, not values; read the value with an argument of this command's sub-commands", n.Name, name)
	}
	im.nodeHooks(n, path)

	rc.Flags, rc.FlagGroups = im.flags(n, path)
	rc.Arguments = im.arguments(n, path)
	if n.Passthrough {
		rc.Passthrough = true
		for _, a := range rc.Arguments {
			a.Passthrough = false
			a.Schema.Separator = ""
		}
	}
	if n.DefaultCmd != nil {
		im.notef(path, imp.Unsupported, "default command %q dropped: rotini runs no command unless it is named; have this command's handler call its code", n.DefaultCmd.Name)
	} else if len(n.Children) > 0 && !hasMethod(n.Target, "Run") {
		im.branchNoRun = true
	}

	inherited := im.inherited
	im.inherited = walk.Cascade(inherited, rc.Flags, im.orig)
	defer func() { im.inherited = inherited }()
	names := walk.Names{}
	for _, child := range n.Children {
		orig := child.Name
		if child.Type == kong.ArgumentNode {
			orig = walk.ArgName(child.Name)
		}
		childName, why := names.Rename(orig)
		switch {
		case why != "":
			im.notef(walk.JoinPath(path, childName), imp.Lossy, "command name %q %s; renamed to %q", child.Name, why, childName)
		case childName != child.Name:
			im.notef(walk.JoinPath(path, childName), imp.Lossy, "command name %q is not a valid rotini name; renamed to %q", child.Name, childName)
		}
		rc.Commands = append(rc.Commands, im.command(child, childName, path))
	}
	return rc
}

// helpText splits Kong's help into a summary and a description: the first line of help is
// the summary, and detail (or the rest of help) the description.
func helpText(help, detail string) (summary, description string) {
	help, detail = text(help), text(detail)
	summary, rest, _ := strings.Cut(help, "\n")
	summary = strings.TrimSpace(summary)
	description = detail
	if description == "" {
		description = strings.TrimSpace(rest)
		if description != "" {
			description = help
		}
	}
	if description == summary {
		description = ""
	}
	return summary, description
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

// hookNames are the methods Kong calls on a node's struct, in the order it calls them.
var hookNames = []string{"BeforeReset", "BeforeResolve", "BeforeApply", "AfterApply", "Run", "AfterRun"}

// nodeHooks records where each lifecycle method of n's struct lives. Kong calls the Before
// and After hooks of every node on the command path, so on a node with sub-commands they
// are the cascading hooks.
func (im *importer) nodeHooks(n *kong.Node, path string) {
	branch := len(n.Children) > 0
	for _, h := range hookNames {
		rotini := h
		switch h {
		case "Run":
		case "AfterRun":
			rotini = "PostRun"
			if branch {
				rotini = "CascadingPostRun"
			}
		default:
			rotini = "PreRun"
			if branch {
				rotini = "CascadingPreRun"
			}
		}
		for _, m := range methods(n.Target, h) {
			im.hooks = append(im.hooks, imp.Hook{
				Path: path, Framework: h, Rotini: rotini, Func: m.name, File: m.file, Line: m.line,
			})
		}
	}
}

type method struct {
	name, file string
	line       int
}

// methods finds the methods named name that Kong calls on v: those declared on v's type or
// its pointer type, and on each struct embedded in it, anonymously or with an embed tag.
// Unexported embedded structs count too: Kong releases before 1.16 call their promoted
// methods.
func methods(v reflect.Value, name string) []method {
	var out []method
	walkEmbedded(v, func(t reflect.Type) {
		if m, ok := declaredMethod(t, name); ok {
			out = append(out, m)
		}
	})
	return out
}

func walkEmbedded(v reflect.Value, visit func(reflect.Type)) {
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if !v.IsValid() {
		return
	}
	visit(v.Type())
	if v.Kind() != reflect.Struct {
		return
	}
	t := v.Type()
	for i := range t.NumField() {
		f := t.Field(i)
		if _, embed := f.Tag.Lookup("embed"); embed || f.Anonymous {
			walkEmbedded(v.Field(i), visit)
		}
	}
}

// declaredMethod returns the method name declared on t or *t itself; a method promoted from
// an embedded type is the compiler's wrapper and is skipped, as Kong skips it.
func declaredMethod(t reflect.Type, name string) (method, bool) {
	for _, tt := range []reflect.Type{t, reflect.PointerTo(t)} {
		m, ok := tt.MethodByName(name)
		if !ok {
			continue
		}
		fn, file, line := walk.FuncPos(m.Func.Interface())
		if file == "<autogenerated>" {
			continue
		}
		return method{fn, file, line}, true
	}
	return method{}, false
}

func hasMethod(v reflect.Value, name string) bool { return len(methods(v, name)) > 0 }

// countGroups counts, for each xor group and "and" group, the nodes that declare its flags.
func (im *importer) countGroups(n *kong.Node) {
	seen := map[string]bool{}
	for _, f := range n.Flags {
		for _, g := range groupKeys(f) {
			if !seen[g] {
				seen[g] = true
				im.groupNodes[g]++
			}
		}
	}
	for _, c := range n.Children {
		im.countGroups(c)
	}
}

// programNotes adds the notes that concern the whole import, once each.
func (im *importer) programNotes(path string) {
	if len(im.missing) > 0 {
		vars := make([]string, 0, len(im.missing))
		for _, v := range im.missing {
			vars = append(vars, "${"+v+"}")
		}
		im.notef(path, imp.Lossy, "Kong variables %s were not supplied: help keeps them as written, and the defaults, enums and environment variables built from them were dropped; supply each one and import again", strings.Join(vars, ", "))
	}
	if im.branchNoRun {
		im.notef(path, imp.Info, "Kong rejects a command that has sub-commands when none is named; rotini runs that command's handler, so make it print help or fail")
	}
	im.notef(path, imp.Info, "options the program passes to kong.Parse or kong.New (kong.Description, kong.Configuration, resolvers, groups, mappers, bindings) were not seen unless given to the import")
	for _, m := range []string{"github.com/alecthomas/kong-yaml", "github.com/alecthomas/kong-toml", "github.com/alecthomas/kong-hcl"} {
		if walk.DependsOn(m) {
			im.notef(path, imp.Lossy, "%s is linked in: Kong configuration loaders were not imported; declare config_files and key: on inputs by hand", m)
		}
	}
	if walk.DependsOn("github.com/willabides/kongplete") {
		im.notef(path, imp.Info, "kongplete is linked in: rotini generates completion scripts from the spec when the completion feature is on")
	}
}
