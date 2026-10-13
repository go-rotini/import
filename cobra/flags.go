package cobra

import (
	"encoding/csv"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	imp "github.com/go-rotini/import"
	"github.com/go-rotini/import/internal/walk"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Cobra's annotation keys. They are written out rather than taken from Cobra so the
// importer builds against Cobra versions that do not declare them.
const (
	annDisplayName = "cobra_annotation_command_display_name"
	annTogether    = "cobra_annotation_required_if_others_set"
	annOneRequired = "cobra_annotation_one_required"
	annExclusive   = "cobra_annotation_mutually_exclusive"
)

// groupAnnotations maps Cobra's flag-group annotations to rotini's group kinds, in a fixed
// order so the import is deterministic.
var groupAnnotations = []struct{ annotation, kind string }{
	{annTogether, "required_together"},
	{annOneRequired, "at_least_one"},
	{annExclusive, "mutually_exclusive"},
}

func setByCobra(f *pflag.Flag) bool {
	return len(f.Annotations[cobra.FlagSetByCobraAnnotation]) > 0
}

func (im *importer) flags(c *cobra.Command, path string) ([]*imp.Flag, []*imp.FlagGroup) {
	isRoot := !c.HasParent()
	var help, version *imp.Flag
	var out []*imp.Flag
	names := map[string]string{} // pflag name -> imported name, for this command's own flags
	used := walk.Names{}
	locals := 0
	add := func(f *pflag.Flag, cascading bool) {
		switch {
		case f.Name == "help" && isRoot:
			help = im.builtinFlag(f, "help", true)
			return
		case f.Name == "help" && setByCobra(f):
			return // the root's cascading help flag covers every command
		case f.Name == "version" && setByCobra(f):
			version = im.builtinFlag(f, "version", false)
			im.notef(path, imp.Info, "Version %q: Cobra's version flag is kept as %s, on this command only; the version string itself belongs in the build (ldflags) or the root hook",
				c.Version, strings.Join(version.Identifiers, "/"))
			return
		}
		rf := im.flag(c, path, f, cascading, used)
		names[f.Name] = rf.Name
		if !cascading {
			locals++
		}
		out = append(out, rf)
	}
	c.PersistentFlags().VisitAll(func(f *pflag.Flag) { add(f, true) })
	c.LocalNonPersistentFlags().VisitAll(func(f *pflag.Flag) { add(f, false) })
	if locals > 0 && hasRealSubCommands(c) {
		im.parentLocalFlags = append(im.parentLocalFlags, path)
	}
	if help != nil {
		out = append([]*imp.Flag{help}, out...)
		if c.Flags().ShorthandLookup("h") != nil && c.Flags().ShorthandLookup("h").Name != "help" {
			im.notef(path, imp.Lossy, "-h belongs to --%s here, so the help flag has no -h", c.Flags().ShorthandLookup("h").Name)
		}
	}
	if version != nil {
		out = append(out, version)
	}
	return out, im.flagGroups(c, path, names)
}

func (im *importer) builtinFlag(f *pflag.Flag, kind string, cascading bool) *imp.Flag {
	var ids []string
	if f.Shorthand != "" {
		ids = append(ids, "-"+f.Shorthand)
	}
	ids = append(ids, "--"+f.Name)
	return &imp.Flag{
		Name:         kind,
		Summary:      f.Usage,
		Identifiers:  ids,
		Cascading:    cascading,
		ShortCircuit: true,
		Schema:       &imp.Schema{Type: "bool"},
		Builtin:      kind,
	}
}

var backquoted = regexp.MustCompile("`([^`]+)`")

func (im *importer) flag(c *cobra.Command, path string, f *pflag.Flag, cascading bool, used walk.Names) *imp.Flag {
	name, renamed := used.RenameFlag(f.Name, im.inherited)
	if renamed != "" {
		im.notef(path, imp.Lossy, "flag --%s %s; renamed to --%s", f.Name, renamed, name)
	}
	rf := &imp.Flag{Name: name, Cascading: cascading, Hidden: f.Hidden, Deprecated: f.Deprecated}
	im.orig[rf] = f.Name
	if f.Deprecated != "" {
		im.deprecations = true
	}
	switch {
	case f.Shorthand == "":
	case walk.IsLetter(rune(f.Shorthand[0])):
		rf.Identifiers = append(rf.Identifiers, "-"+f.Shorthand)
		if f.ShorthandDeprecated != "" {
			rf.DeprecatedIdentifiers = []string{"-" + f.Shorthand}
			im.deprecations = true
			im.notef(path, imp.Lossy, "-%s is deprecated; its message %q is not carried across", f.Shorthand, f.ShorthandDeprecated)
		}
	default:
		im.notef(path, imp.Lossy, "-%s dropped from --%s: a short flag must be a letter", f.Shorthand, name)
	}
	rf.Identifiers = append(rf.Identifiers, "--"+name)

	s := im.flagSchema(path, f, name)
	rf.Schema = s
	if m := backquoted.FindStringSubmatch(f.Usage); m != nil {
		s.Placeholder = m[1]
	}
	rf.Summary = strings.TrimSpace(strings.ReplaceAll(f.Usage, "`", ""))

	if v := f.Annotations[cobra.BashCompOneRequiredFlag]; len(v) > 0 && v[0] == "true" {
		s.Required = true
		if s.Default != nil {
			im.notef(path, imp.Lossy, "--%s is required and has the default %v: the default was dropped, since Cobra demands the flag on the command line anyway", name, s.Default)
			s.Default = nil
		}
	}
	if exts, ok := f.Annotations[cobra.BashCompFilenameExt]; ok {
		cc := &imp.Complete{Kind: "file"}
		for _, e := range exts {
			if e = strings.TrimPrefix(strings.TrimPrefix(e, "*"), "."); e != "" {
				cc.Extensions = append(cc.Extensions, e)
			}
		}
		s.Complete = cc
	}
	if dirs, ok := f.Annotations[cobra.BashCompSubdirsInDir]; ok {
		s.Complete = &imp.Complete{Kind: "directory"}
		if len(dirs) > 0 {
			im.notef(path, imp.Lossy, "--%s completes the sub-directories of %v in Cobra; rotini completes any directory", name, dirs)
		}
	}
	if _, ok := f.Annotations[cobra.BashCompCustom]; ok {
		im.notef(path, imp.Unsupported, "--%s uses a legacy bash completion function, which was dropped", name)
	}
	if fn, ok := flagCompletion(c, f.Name); ok {
		im.completion(c, path, "--"+name, fn, s)
	}
	return rf
}

// pflagTypes maps pflag's value type names to rotini types. A slice or map type also takes
// a comma separator, as pflag splits it on commas.
var pflagTypes = map[string]struct {
	rotini string
	csv    bool
}{
	"bool": {"bool", false}, "string": {"string", false}, "count": {"count", false},
	"int": {"int", false}, "int8": {"int8", false}, "int16": {"int16", false},
	"int32": {"int32", false}, "int64": {"int64", false},
	"uint": {"uint", false}, "uint8": {"uint8", false}, "uint16": {"uint16", false},
	"uint32": {"uint32", false}, "uint64": {"uint64", false},
	"float32": {"float32", false}, "float64": {"float64", false},
	"duration": {"duration", false}, "ip": {"ip", false}, "ipNet": {"cidr", false},
	"bytesHex": {"hexbytes", false}, "bytesBase64": {"base64bytes", false},
	"stringArray": {"[]string", false}, "stringSlice": {"[]string", true},
	"intSlice": {"[]int", true}, "int32Slice": {"[]int32", true}, "int64Slice": {"[]int64", true},
	"uintSlice": {"[]uint", true}, "float32Slice": {"[]float32", true}, "float64Slice": {"[]float64", true},
	"boolSlice": {"[]bool", true}, "durationSlice": {"[]duration", true},
	"ipSlice": {"[]ip", true}, "ipNetSlice": {"[]cidr", true},
	"stringToString": {"map[string]string", true}, "stringToInt": {"map[string]int", true},
	"stringToInt64": {"map[string]int64", true},
}

func (im *importer) flagSchema(path string, f *pflag.Flag, name string) *imp.Schema {
	t := f.Value.Type()
	s := &imp.Schema{}
	if m, ok := pflagTypes[t]; ok {
		s.Type = m.rotini
		if m.csv {
			s.Separator = ","
		}
	} else {
		switch t {
		case "time":
			s.Type = "time"
			im.notef(path, imp.Lossy, "--%s is a time flag: the formats it accepts are not exported; set layout: if they are not RFC 3339", name)
		case "boolfunc":
			s.Type = "bool"
			im.notef(path, imp.Lossy, "--%s calls a function when set: imported as bool, move the function into the handler", name)
		case "func":
			s.Type = "string"
			im.notef(path, imp.Lossy, "--%s calls a function with its value: imported as string, move the function into the handler", name)
		case "ipMask":
			s.Type = "string"
			im.notef(path, imp.Unsupported, "--%s is an IP mask: rotini has no mask type, so it was imported as string", name)
		default:
			s.Type = "string"
			im.notef(path, imp.Lossy, "--%s has the custom value type %q (%T): imported as string; use a type with import: and a TextUnmarshaler, or an enum", name, t, f.Value)
		}
	}
	s.Default = defaultValue(t, f.DefValue)
	if f.NoOptDefVal != "" {
		im.implicitValue(path, f, name, t, s)
	}
	return s
}

func (im *importer) implicitValue(path string, f *pflag.Flag, name, t string, s *imp.Schema) {
	switch {
	case t == "count", t == "boolfunc", t == "bool" && f.NoOptDefVal == "true":
	case t == "bool":
		im.notef(path, imp.Unsupported, "--%s is a bool whose bare form means %q, which rotini cannot express", name, f.NoOptDefVal)
	case strings.HasPrefix(s.Type, "[]") || strings.HasPrefix(s.Type, "map"):
		im.notef(path, imp.Unsupported, "--%s has a value for its bare form on a list or map flag, which rotini cannot express", name)
	default:
		s.ImplicitValue = f.NoOptDefVal
	}
}

// zeroDefault reports whether dv is how pflag writes the zero value of a flag of the type
// t, which means "no default": rotini leaves an unset input at its zero value, as pflag
// does. A type pflag does not declare may write its zero value any of these ways.
func zeroDefault(t, dv string) bool {
	if dv == "" {
		return true
	}
	rt, known := pflagTypes[t]
	switch {
	case t == "time":
		return dv == "0001-01-01T00:00:00Z"
	case !known:
		switch dv {
		case "false", "0", "0s", "[]", "<nil>", "0.0", "map[]", "0001-01-01T00:00:00Z":
			return true
		}
		return false
	case strings.HasPrefix(rt.rotini, "[]") || strings.HasPrefix(rt.rotini, "map"):
		return dv == "[]" || dv == "map[]"
	}
	switch rt.rotini {
	case "bool":
		return dv == "false"
	case "count", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64":
		return dv == "0"
	case "float32", "float64":
		return dv == "0" || dv == "0.0"
	case "duration":
		return dv == "0s"
	case "ip", "cidr":
		return dv == "<nil>"
	}
	return false
}

// defaultValue turns pflag's DefValue string into a default, typed for the flag.
func defaultValue(t, dv string) any {
	if zeroDefault(t, dv) {
		return nil
	}
	rt := pflagTypes[t].rotini
	isList := strings.HasPrefix(rt, "[]") || strings.HasPrefix(rt, "map")
	if isList && strings.HasPrefix(dv, "[") && strings.HasSuffix(dv, "]") {
		items := splitList(dv[1 : len(dv)-1])
		if elem, ok := strings.CutPrefix(t, "stringTo"); ok {
			elem = strings.ToLower(elem)
			m := map[string]any{}
			for _, kv := range items {
				k, v, _ := strings.Cut(kv, "=")
				m[k] = scalarValue(elem, v)
			}
			return m
		}
		out := make([]any, 0, len(items))
		for _, it := range items {
			out = append(out, scalarValue(strings.TrimSuffix(t, "Slice"), it))
		}
		return out
	}
	return scalarValue(t, dv)
}

// splitList splits a pflag list default, which pflag writes as CSV.
func splitList(body string) []string {
	r := csv.NewReader(strings.NewReader(body))
	r.LazyQuotes = true
	rec, err := r.Read()
	if err != nil {
		return strings.Split(body, ",")
	}
	return rec
}

func scalarValue(t, v string) any {
	switch t {
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "count":
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			return n
		}
	case "float32", "float64":
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			return n
		}
	case "bool":
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return v
}

// flagGroups collects the flag groups this command declares: those whose every member is
// one of its own flags. pflag shares a flag between the commands that inherit it, so a group
// shows on every command that sees its flags; a group over inherited flags only belongs to
// an ancestor, and one that mixes own and inherited flags cannot be expressed.
func (im *importer) flagGroups(c *cobra.Command, path string, names map[string]string) []*imp.FlagGroup {
	keys := im.ownGroups(c, path, names)
	kinds := map[string]map[string]bool{}
	for _, k := range keys {
		if kinds[k.set] == nil {
			kinds[k.set] = map[string]bool{}
		}
		kinds[k.set][k.kind] = true
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].set != keys[j].set {
			return keys[i].set < keys[j].set
		}
		return keys[i].kind < keys[j].kind
	})
	var out []*imp.FlagGroup
	merged := map[string]bool{}
	for _, k := range keys {
		kind := k.kind
		// at_least_one and mutually_exclusive over one set is exactly one: rotini's one_of.
		if ks := kinds[k.set]; ks["at_least_one"] && ks["mutually_exclusive"] && kind != "required_together" {
			if merged[k.set] {
				continue
			}
			merged[k.set] = true
			kind = "one_of"
		}
		var flags []string
		for _, m := range strings.Fields(k.set) {
			flags = append(flags, names[m])
		}
		out = append(out, &imp.FlagGroup{Kind: kind, Flags: flags})
	}
	return out
}

// groupKey is one flag group: its kind and its sorted pflag names, space-joined.
type groupKey struct{ kind, set string }

// ownGroups reads the group annotations on the flags c sees and keeps the groups made only
// of c's own flags (names holds them).
func (im *importer) ownGroups(c *cobra.Command, path string, names map[string]string) []groupKey {
	seen := map[groupKey]bool{}
	var keys []groupKey
	c.Flags().VisitAll(func(f *pflag.Flag) {
		for _, ga := range groupAnnotations {
			for _, g := range f.Annotations[ga.annotation] {
				members := strings.Fields(g)
				sort.Strings(members)
				k := groupKey{ga.kind, strings.Join(members, " ")}
				if seen[k] {
					continue
				}
				seen[k] = true
				present, own := 0, 0
				for _, m := range members {
					if c.Flags().Lookup(m) != nil {
						present++
					}
					if _, ok := names[m]; ok {
						own++
					}
				}
				switch {
				case present < len(members) || own == 0:
				case own == len(members):
					keys = append(keys, k)
				case !im.groupsNoted[k]:
					im.groupsNoted[k] = true
					im.notef(path, imp.Unsupported, "%s group [%s] mixes this command's flags with inherited ones: a rotini group names flags of one command, so it was dropped", k.kind, k.set)
				}
			}
		}
	})
	return keys
}

// completion maps a Go completion function. cobra.NoFileCompletions is rotini's complete
// kind none; anything else is reported, since rotini completes values from an enum or from
// a completer written for rotini.
func (im *importer) completion(c *cobra.Command, path, what string, fn completionFunc, s *imp.Schema) {
	name := walk.FuncName(fn)
	switch {
	case name == "github.com/spf13/cobra.NoFileCompletions":
		s.Complete = &imp.Complete{Kind: "none"}
	case strings.HasPrefix(name, "github.com/spf13/cobra.FixedCompletions"):
		im.notef(path, imp.Lossy, "%s completes the fixed list %v: add enum: if the set is closed", what, fixedValues(c, fn))
	default:
		im.notef(path, imp.Lossy, "%s has a Go completion function (%s): reimplement it as a rotini completer", what, name)
	}
}

func fixedValues(c *cobra.Command, fn completionFunc) (vals []string) {
	defer func() {
		if recover() != nil {
			vals = nil
		}
	}()
	got, _ := fn(c, nil, "")
	for _, v := range got {
		value, _, _ := strings.Cut(v, "\t")
		vals = append(vals, value)
	}
	return vals
}

// completionFunc is Cobra's completion function type, spelled out: Cobra 1.9 names it
// CompletionFunc, as an alias of this same type.
type completionFunc = func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective)

type flagCompletionLookup interface {
	GetFlagCompletionFunc(name string) (completionFunc, bool)
}

func hasFlagCompletionLookup(c *cobra.Command) bool {
	_, ok := any(c).(flagCompletionLookup)
	return ok
}

// flagCompletion returns the completion function registered for a flag, on Cobra versions
// that expose it (1.8 and later).
func flagCompletion(c *cobra.Command, name string) (completionFunc, bool) {
	l, ok := any(c).(flagCompletionLookup)
	if !ok {
		return nil, false
	}
	fn, ok := l.GetFlagCompletionFunc(name)
	return fn, ok && fn != nil
}

func hasRealSubCommands(c *cobra.Command) bool {
	for _, sub := range c.Commands() {
		if !strings.HasPrefix(sub.Name(), "__complete") && !isDefaultHelpCmd(sub) && !isDefaultCompletionCmd(sub) {
			return true
		}
	}
	return false
}

func (im *importer) argCompletion(c *cobra.Command, path string, args []*imp.Argument) {
	if c.ValidArgsFunction == nil {
		return
	}
	if len(args) == 0 {
		im.notef(path, imp.Info, "ValidArgsFunction on a command that takes no arguments: ignored")
		return
	}
	for _, a := range args {
		im.completion(c, path, fmt.Sprintf("<%s>", a.Name), c.ValidArgsFunction, a.Schema)
	}
}
