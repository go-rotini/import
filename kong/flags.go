package kong

import (
	"encoding"
	"encoding/json"
	"net"
	"net/netip"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/alecthomas/kong"
	imp "github.com/go-rotini/import"
	"github.com/go-rotini/import/internal/walk"
)

var (
	versionFlagType = reflect.TypeFor[kong.VersionFlag]()
	// decoders are the interfaces through which a type decodes itself in Kong.
	decoders = []reflect.Type{
		reflect.TypeFor[kong.MapperValue](),
		reflect.TypeFor[encoding.TextUnmarshaler](),
		reflect.TypeFor[encoding.BinaryUnmarshaler](),
		reflect.TypeFor[json.Unmarshaler](),
	}
)

// knownTypes maps the Go types Kong decodes, by type or by a stdlib unmarshaler, to rotini
// types.
var knownTypes = map[reflect.Type]string{
	reflect.TypeFor[time.Duration]():      "duration",
	reflect.TypeFor[time.Time]():          "time",
	reflect.TypeFor[*url.URL]():           "url",
	reflect.TypeFor[net.IP]():             "ip",
	reflect.TypeFor[netip.Addr]():         "ip",
	reflect.TypeFor[netip.Prefix]():       "cidr",
	reflect.TypeFor[netip.AddrPort]():     "hostport",
	reflect.TypeFor[*regexp.Regexp]():     "regexp",
	reflect.TypeFor[*os.File]():           "inputfile",
	reflect.TypeFor[kong.ConfigFlag]():    "string",
	reflect.TypeFor[kong.ChangeDirFlag](): "existingdir",
}

var envName = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

func (im *importer) flags(n *kong.Node, path string) ([]*imp.Flag, []*imp.FlagGroup) {
	cascading := len(n.Children) > 0
	out := make([]*imp.Flag, 0, len(n.Flags))
	var extra []*imp.Flag        // the negated forms of non-bool negatable flags
	names := map[string]string{} // Kong flag name -> imported name
	used := walk.Names{}
	for _, f := range n.Flags {
		var rf *imp.Flag
		switch {
		case im.app.Model.HelpFlag != nil && f == im.app.Model.HelpFlag:
			rf = builtinFlag(f, "help", true)
		case f.Target.Type() == versionFlagType:
			rf = builtinFlag(f, "version", cascading)
			im.notef(path, imp.Info, "%s prints the Kong variable ${version}; the version string belongs in the build (ldflags) or the root hook", strings.Join(rf.Identifiers, "/"))
		default:
			var negated *imp.Flag
			rf, negated = im.flag(f, path, cascading, used)
			if negated != nil {
				extra = append(extra, negated)
			}
		}
		names[f.Name] = rf.Name
		out = append(out, rf)
	}
	groups := im.flagGroups(n, path, out, names)
	for _, nf := range extra {
		name, why := used.RenameFlag(nf.Name, im.inherited)
		im.orig[nf] = nf.Name
		if why != "" {
			im.notef(path, imp.Lossy, "the flag for %s %s; it was named --%s", nf.Identifiers[0], why, name)
		}
		nf.Name = name
		out = append(out, nf)
	}
	return out, groups
}

func builtinFlag(f *kong.Flag, kind string, cascading bool) *imp.Flag {
	var ids []string
	if f.Short != 0 {
		ids = append(ids, "-"+string(f.Short))
	}
	ids = append(ids, "--"+f.Name)
	return &imp.Flag{
		Name:         kind,
		Summary:      text(f.Help),
		Identifiers:  ids,
		Cascading:    cascading,
		ShortCircuit: true,
		Hidden:       f.Hidden,
		Schema:       &imp.Schema{Type: "bool"},
		Builtin:      kind,
	}
}

// flag maps one flag. A negatable flag that is not a bool also gives negated, a bool flag of
// its own for the negated form, since a rotini negatable flag is a bool.
func (im *importer) flag(f *kong.Flag, path string, cascading bool, used walk.Names) (rf, negated *imp.Flag) {
	name, why := used.RenameFlag(f.Name, im.inherited)
	if why != "" {
		im.notef(path, imp.Lossy, "flag --%s %s; renamed to --%s", f.Name, why, name)
	}
	rf = &imp.Flag{Name: name, Summary: text(f.Help), Cascading: cascading, Hidden: f.Hidden}
	im.orig[rf] = f.Name
	if f.Group != nil {
		rf.Group = text(f.Group.Title)
	}
	switch {
	case f.Short == 0:
	case f.Short < unicode.MaxASCII && walk.IsLetter(f.Short):
		rf.Identifiers = append(rf.Identifiers, "-"+string(f.Short))
	default:
		im.notef(path, imp.Lossy, "-%c dropped from --%s: a short flag must be a letter", f.Short, name)
	}
	rf.Identifiers = append(rf.Identifiers, "--"+name)
	for _, a := range f.Aliases {
		r, _ := utf8.DecodeRuneInString(a)
		switch n := utf8.RuneCountInString(a); {
		case n == 1 && walk.IsLetter(r):
			rf.Identifiers = append(rf.Identifiers, "-"+a)
		case n > 1 && walk.ValidName(a):
			rf.Identifiers = append(rf.Identifiers, "--"+a)
		default:
			im.notef(path, imp.Lossy, "alias %q dropped from --%s: not a valid rotini identifier", a, name)
		}
	}

	what := "--" + name
	s := im.schema(f.Value, path, what, true)
	rf.Schema = s
	if f.PlaceHolder != "" {
		s.Placeholder = text(f.PlaceHolder)
	}
	if neg := negatable(f); neg != "" {
		switch {
		case s.Type != "bool":
			negated = negatedFlag(rf, neg)
			im.notef(path, imp.Lossy, "%s is negatable but its type is %s, and a rotini negatable flag is a bool: %s was imported as a bool flag of its own; when it is given, set %s to its negated value in the handler", what, s.Type, neg, what)
		case neg == "--no-"+f.Name:
			s.Negatable = true
		default:
			s.Negatable = neg
		}
	}
	for _, e := range f.Envs {
		switch {
		case hasPlaceholder(e):
		case envName.MatchString(e):
			s.Variable = append(s.Variable, e)
		default:
			im.notef(path, imp.Lossy, "%s reads the environment variable %q, which is not a valid rotini variable name; it was dropped", what, e)
		}
	}
	if s.Required && s.Default != nil {
		im.notef(path, imp.Lossy, "%s is required and has the default %v: the default was dropped, since rotini rejects both together", what, s.Default)
		s.Default = nil
	}
	im.valueHooks(f.Value, path, what)
	return rf, negated
}

// negatedFlag is the bool flag that stands for the negated form neg of the non-bool flag rf.
func negatedFlag(rf *imp.Flag, neg string) *imp.Flag {
	return &imp.Flag{
		Name:        walk.CommandName(neg),
		Summary:     "negated form of --" + rf.Name,
		Identifiers: []string{neg},
		Group:       rf.Group,
		Cascading:   rf.Cascading,
		Hidden:      rf.Hidden,
		Schema:      &imp.Schema{Type: "bool"},
	}
}

// negatable returns the negated identifier of a negatable flag. Kong's tag holds a bool in
// releases before 1.2 and the negated name after.
func negatable(f *kong.Flag) string {
	v := reflect.ValueOf(f.Tag).Elem().FieldByName("Negatable")
	switch {
	case !v.IsValid():
	case v.Kind() == reflect.Bool && v.Bool():
		return "--no-" + f.Name
	case v.Kind() == reflect.String && v.String() == "_":
		return "--no-" + f.Name
	case v.Kind() == reflect.String && v.String() != "":
		return "--" + v.String()
	}
	return ""
}

// schema maps a flag's or a positional's value: its type, default, enum, separator and
// completion hint.
func (im *importer) schema(v *kong.Value, path, what string, isFlag bool) *imp.Schema {
	t := v.Target.Type()
	s := &imp.Schema{Required: v.Required}
	tagType := v.Tag.Type
	switch tagType {
	case "counter":
		s.Type = "count"
		if v.HasDefault && !hasPlaceholder(v.Default) {
			switch n, err := strconv.ParseInt(v.Default, 10, 64); {
			case err != nil:
				im.notef(path, imp.Lossy, "%s: the counter default %q is not a decimal whole number, so it was dropped", what, v.Default)
			case n != 0:
				s.Default = n
			}
		}
		return s
	case "", "path", "existingfile", "existingdir", "filecontent":
	default:
		s.Type = "string"
		im.notef(path, imp.Lossy, "%s uses the Kong mapper %q: imported as string; use a type with import: and a TextUnmarshaler, or an enum", what, tagType)
		return s
	}

	elem, kind := t, ""
	switch {
	case t.Kind() == reflect.Map && t.Key().Kind() == reflect.String:
		elem, kind = t.Elem(), "map"
	case t.Kind() == reflect.Slice && knownTypes[t] == "" && t != reflect.TypeFor[[]byte]():
		elem, kind = t.Elem(), "slice"
	}
	base, ok := goType(elem)
	if !ok {
		base = "string"
		im.notef(path, imp.Lossy, "%s has the custom type %s: imported as string; use a type with import: and a TextUnmarshaler, or an enum", what, elem)
	}
	base = im.special(v, s, elem, base, path, what)

	sep, mapSep := separator(v.Tag.Sep), separator(v.Tag.MapSep)
	switch kind {
	case "map":
		s.Type = "map[string]" + base
		s.Separator = mapSep
	case "slice":
		s.Type = "[]" + base
		if isFlag || v.Tag.Has("sep") {
			s.Separator = sep
		}
	default:
		s.Type = base
	}
	if kind == "slice" && v.Required {
		one := 1
		s.MinItems = &one
	}

	im.enum(v, s, path, what)
	if v.HasDefault {
		switch {
		case hasPlaceholder(v.Default):
			im.notef(path, imp.Lossy, "%s: its default comes from a Kong variable that was not supplied, so it was dropped", what)
		case v.Default != "":
			s.Default = defaultValue(kind, base, v.Default, sep, mapSep)
		}
	}
	return s
}

// special maps what a Kong type name or one of Kong's own types adds to the base type, and
// returns the base type.
func (im *importer) special(v *kong.Value, s *imp.Schema, elem reflect.Type, base, path, what string) string {
	switch v.Tag.Type {
	case "path":
		s.Complete = &imp.Complete{Kind: "file"}
		im.notef(path, imp.Lossy, "%s is a Kong path: Kong expands ~ and makes it absolute; declare expand: [home] and resolve it in the handler", what)
	case "existingfile", "existingdir":
		base = v.Tag.Type
	case "filecontent":
		base = "string"
		im.notef(path, imp.Lossy, "%s reads the named file's content in Kong: imported as the file name; read it in the handler, or declare from: [file]", what)
	}
	switch elem {
	case reflect.TypeFor[*os.File]():
		im.notef(path, imp.Lossy, "%s opens the named file in Kong: rotini binds the path; open it with rotini.OpenInput", what)
	case reflect.TypeFor[kong.ConfigFlag]():
		s.Complete = &imp.Complete{Kind: "file"}
		im.notef(path, imp.Lossy, "%s is a kong.ConfigFlag, which loads a configuration file: declare config_files with config_source: naming this flag", what)
	case reflect.TypeFor[kong.ChangeDirFlag]():
		im.notef(path, imp.Lossy, "%s is a kong.ChangeDirFlag, which changes the working directory before the run: join paths to it in the handler", what)
	case reflect.TypeFor[time.Time]():
		if v.Format != "" {
			im.notef(path, imp.Lossy, "%s parses times with the layout %q: declare it as layout:", what, v.Format)
		}
	}
	return base
}

// separator returns a separator Kong read from a tag, or "" for "none".
func separator(r rune) string {
	if r < 0 {
		return ""
	}
	return string(r)
}

func (im *importer) enum(v *kong.Value, s *imp.Schema, path, what string) {
	if v.Enum == "" {
		return
	}
	if hasPlaceholder(v.Enum) {
		im.notef(path, imp.Lossy, "%s: its enum comes from a Kong variable that was not supplied, so it was dropped", what)
		return
	}
	var vals []imp.EnumValue
	for _, e := range strings.Split(v.Enum, ",") {
		vals = append(vals, imp.EnumValue{Value: strings.TrimSpace(e)})
	}
	if elem, ok := strings.CutPrefix(s.Type, "[]"); ok {
		s.Items = &imp.Schema{Type: elem, Enum: vals}
		return
	}
	s.Enum = vals
}

// goType maps a Go type Kong decodes to a rotini type. A type with its own decoding (a
// Kong MapperValue or an unmarshaler) that rotini does not know is not mapped.
func goType(t reflect.Type) (string, bool) {
	if rt, ok := knownTypes[t]; ok {
		return rt, true
	}
	if t.Kind() == reflect.Pointer {
		if rt, ok := knownTypes[t.Elem()]; ok {
			return rt, true
		}
		t = t.Elem()
	}
	for _, impl := range []reflect.Type{t, reflect.PointerTo(t)} {
		if slices.ContainsFunc(decoders, impl.Implements) {
			return "", false
		}
	}
	switch t.Kind() {
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return t.Kind().String(), true
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return "string", true
		}
	}
	return "", false
}

// defaultValue types Kong's default text for the input: a number or bool for those types,
// a list split on the separator, a map split on the map separator and "=".
func defaultValue(kind, base, def, sep, mapSep string) any {
	switch kind {
	case "slice":
		items := splitEscaped(def, sep)
		out := make([]any, 0, len(items))
		for _, it := range items {
			out = append(out, scalarValue(base, it))
		}
		return out
	case "map":
		items := splitEscaped(def, mapSep)
		m := map[string]any{}
		for _, kv := range items {
			k, v, _ := strings.Cut(kv, "=")
			m[k] = scalarValue(base, v)
		}
		return m
	}
	return scalarValue(base, def)
}

func scalarValue(t, v string) any {
	switch t {
	case "int", "int8", "int16", "int32", "int64":
		if n, err := strconv.ParseInt(v, 0, 64); err == nil {
			return n
		}
	case "uint", "uint8", "uint16", "uint32", "uint64":
		if n, err := strconv.ParseUint(v, 0, 64); err == nil {
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

// valueHooks records the hook methods of a flag's own type, which Kong calls when the flag
// is given. Kong's own flag types are mapped elsewhere.
func (im *importer) valueHooks(v *kong.Value, path, what string) {
	t := v.Target.Type()
	if t.PkgPath() == kongPkg {
		return
	}
	for _, h := range []string{"BeforeReset", "BeforeResolve", "BeforeApply", "AfterApply"} {
		m, ok := declaredMethod(t, h)
		if !ok {
			continue
		}
		im.hooks = append(im.hooks, imp.Hook{
			Path: path, Framework: what + " " + h, Rotini: "PreRun", Func: m.name, File: m.file, Line: m.line,
		})
		im.notef(path, imp.Lossy, "%s runs %s when it is given: port it to the PreRun hook, checking the flag", what, m.name)
	}
}

// groupKeys names the xor groups and the "and" groups f belongs to, kind-prefixed.
func groupKeys(f *kong.Flag) []string {
	ands := andGroups(f)
	out := make([]string, 0, len(f.Xor)+len(ands))
	for _, g := range f.Xor {
		out = append(out, "xor "+g)
	}
	for _, g := range ands {
		out = append(out, "and "+g)
	}
	return out
}

// andGroups reads a flag's and groups; Kong releases before 1.0 have none.
func andGroups(f *kong.Flag) []string {
	v := reflect.ValueOf(f).Elem().FieldByName("And")
	if !v.IsValid() {
		return nil
	}
	out, _ := v.Interface().([]string)
	return out
}

// flagGroups maps the xor groups and the "and" groups whose flags are all declared on n. An xor group of
// required flags is Kong's "exactly one of", rotini's one_of.
func (im *importer) flagGroups(n *kong.Node, path string, flags []*imp.Flag, names map[string]string) []*imp.FlagGroup {
	type group struct {
		key     string
		members []int // indexes into n.Flags
	}
	var groups []*group
	byKey := map[string]*group{}
	for i, f := range n.Flags {
		for _, k := range groupKeys(f) {
			g := byKey[k]
			if g == nil {
				g = &group{key: k}
				byKey[k] = g
				groups = append(groups, g)
			}
			g.members = append(g.members, i)
		}
	}
	var out []*imp.FlagGroup
	for _, g := range groups {
		kind, name, _ := strings.Cut(g.key, " ")
		if im.groupNodes[g.key] > 1 {
			im.notef(path, imp.Unsupported, "%s group %q spans flags of more than one command: a rotini group names flags of one command, so it was dropped", kind, name)
			continue
		}
		if len(g.members) < 2 {
			continue
		}
		fg := &imp.FlagGroup{Kind: "mutually_exclusive"}
		required := 0
		for _, i := range g.members {
			fg.Flags = append(fg.Flags, names[n.Flags[i].Name])
			if n.Flags[i].Required {
				required++
			}
		}
		if kind == "and" {
			// Kong requires every flag of an and group when it requires one.
			for _, i := range g.members {
				flags[i].Schema.Required = required > 0
			}
			fg.Kind = "required_together"
			out = append(out, fg)
			continue
		}
		if required > 0 {
			fg.Kind = "one_of"
			for _, i := range g.members {
				flags[i].Schema.Required = false
			}
			if required < len(g.members) {
				im.notef(path, imp.Lossy, "xor group %q has required and optional flags: imported as one_of, which requires exactly one", name)
			}
		}
		out = append(out, fg)
	}
	return out
}

// splitEscaped splits s on sep as Kong does, honoring a backslash before sep. An empty sep
// keeps s whole.
func splitEscaped(s, sep string) []string {
	if sep == "" {
		return []string{s}
	}
	r, _ := utf8.DecodeRuneInString(sep)
	return kong.SplitEscaped(s, r)
}
