package urfave

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"

	imp "github.com/go-rotini/import"
	"github.com/go-rotini/import/internal/walk"
	"github.com/urfave/cli/v3"
)

// The optional flag interfaces, spelled out so the importer builds against every v3 release.
type (
	usageFlag      interface{ GetUsage() string }
	envFlag        interface{ GetEnvVars() []string }
	requiredFlag   interface{ IsRequired() bool }
	visibleFlag    interface{ IsVisible() bool }
	categoryFlag   interface{ GetCategory() string }
	deprecatedFlag interface{ GetDeprecated() string }
	localFlag      interface{ IsLocal() bool }
	envSource      interface {
		IsFromEnv() bool
		Key() string
	}
)

var (
	backquoted = regexp.MustCompile("`([^`]+)`")
	envName    = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
)

// allFlags lists c's flags and then the flags of its mutually exclusive groups, as
// urfave/cli parses them.
func allFlags(c *cli.Command) []cli.Flag {
	flags := append([]cli.Flag(nil), c.Flags...)
	for _, g := range c.MutuallyExclusiveFlags {
		for _, opt := range g.Flags {
			flags = append(flags, opt...)
		}
	}
	return flags
}

func (im *importer) flags(c *cli.Command, path string, hasSubs bool) ([]*imp.Flag, []*imp.FlagGroup) {
	var out []*imp.Flag
	names := map[any]string{} // flagKey -> imported name
	used := walk.Names{}
	locals := 0
	groupCategory := map[any]string{}
	for _, g := range c.MutuallyExclusiveFlags {
		for _, opt := range g.Flags {
			for _, f := range opt {
				if k := flagKey(f); k != nil {
					groupCategory[k] = g.Category
				}
			}
		}
	}
	for _, f := range allFlags(c) {
		if len(flagNames(f)) == 0 {
			im.notef(path, imp.Unsupported, "a flag of type %T has no name, so it was dropped", f)
			continue
		}
		k := flagKey(f)
		if _, seen := names[k]; seen && k != nil {
			continue
		}
		rf := im.flag(f, path, hasSubs, used)
		if rf.Group == "" && k != nil {
			rf.Group = groupCategory[k]
		}
		if !rf.Cascading {
			locals++
		}
		if k != nil {
			names[k] = rf.Name
		}
		out = append(out, rf)
	}
	if locals > 0 && hasSubs {
		im.parentLocalFlags = append(im.parentLocalFlags, path)
	}
	return out, im.flagGroups(c, path, names)
}

// flagKey identifies a flag for the maps the import keeps: its pointer, or the flag itself
// when it is a comparable value. A flag stored by a value that cannot be compared has no key,
// so it is never taken for another, and a group naming it loses it.
func flagKey(f cli.Flag) any {
	v := reflect.ValueOf(f)
	switch {
	case v.Kind() == reflect.Pointer:
		return v.Pointer()
	case v.Comparable():
		return f
	}
	return nil
}

// flagNames lists the names a flag declares, first its own. A BoolWithInverseFlag's Names
// also holds its negated names, which are not identifiers of their own.
func flagNames(f cli.Flag) []string {
	if bf, ok := f.(*cli.BoolWithInverseFlag); ok {
		return append([]string{bf.Name}, bf.Aliases...)
	}
	return f.Names()
}

// identifiers turns urfave/cli flag names into rotini identifiers: one-letter names are
// short identifiers, listed first.
func (im *importer) identifiers(names []string, path, flag string) []string {
	var short, long []string
	for _, n := range names {
		switch {
		case len(n) == 1 && walk.IsLetter(rune(n[0])):
			short = append(short, "-"+n)
		case len(n) > 1 && walk.ValidName(n):
			long = append(long, "--"+n)
		default:
			im.notef(path, imp.Lossy, "the name %q of --%s dropped: not a valid rotini identifier", n, flag)
		}
	}
	return append(short, long...)
}

func (im *importer) flag(f cli.Flag, path string, hasSubs bool, used walk.Names) *imp.Flag {
	names := flagNames(f)
	name, why := used.RenameFlag(names[0], im.inherited)
	if why != "" {
		im.notef(path, imp.Lossy, "flag --%s %s; renamed to --%s", names[0], why, name)
	}
	what := "--" + name
	rf := &imp.Flag{Name: name, Identifiers: im.identifiers(append([]string{name}, names[1:]...), path, name)}
	im.orig[rf] = names[0]
	placeholder := ""
	if u, ok := f.(usageFlag); ok {
		usage := u.GetUsage()
		rf.Summary = strings.TrimSpace(strings.ReplaceAll(usage, "`", ""))
		if m := backquoted.FindStringSubmatch(usage); m != nil {
			placeholder = m[1]
		}
	}
	if v, ok := f.(visibleFlag); ok {
		rf.Hidden = !v.IsVisible()
	}
	if cf, ok := f.(categoryFlag); ok {
		rf.Group = strings.TrimSpace(cf.GetCategory())
	}
	if d, ok := f.(deprecatedFlag); ok && d.GetDeprecated() != "" {
		rf.Deprecated = d.GetDeprecated()
		im.deprecations = true
	}
	if l, ok := f.(localFlag); ok && hasSubs {
		rf.Cascading = !l.IsLocal()
	}

	s := im.flagSchema(f, path, what)
	s.Placeholder = placeholder
	rf.Schema = s
	if r, ok := f.(requiredFlag); ok && r.IsRequired() {
		s.Required = true
		if s.Default != nil {
			im.notef(path, imp.Lossy, "%s is required and has the default %v: the default was dropped, since rotini rejects both together", what, s.Default)
			s.Default = nil
		}
	}
	im.sources(f, s, path, what)
	if fieldBool(f, "TakesFile") {
		s.Complete = &imp.Complete{Kind: "file"}
	}
	if fieldBool(f, "OnlyOnce") {
		switch {
		case strings.HasPrefix(s.Type, "[]"), strings.HasPrefix(s.Type, "map"), s.Type == "count":
			im.notef(path, imp.Lossy, "%s may be given only once (OnlyOnce): rotini cannot limit a list, map or count flag; check it in the handler", what)
		default:
			once := false
			s.Repeatable = &once
		}
	}
	if fn := fieldFunc(f, "Action"); fn != nil {
		im.notef(path, imp.Lossy, "%s runs %s when it is given (Action): move it into the handler", what, walk.FuncName(fn))
	}
	if fn := fieldFunc(f, "Validator"); fn != nil {
		im.notef(path, imp.Lossy, "%s checks its value with %s (Validator): move the check into the handler", what, walk.FuncName(fn))
	}
	return rf
}

// sources maps the flag's value sources: environment variables become the flag's variables,
// and any other source is reported.
func (im *importer) sources(f cli.Flag, s *imp.Schema, path, what string) {
	var envs []string
	src := field(f, "Sources")
	chain, ok := reflect.Value{}, false
	if src.IsValid() {
		chain = src.FieldByName("Chain")
		ok = chain.IsValid() && chain.Kind() == reflect.Slice
	}
	switch {
	case ok:
		for i := range chain.Len() {
			vs := chain.Index(i).Interface()
			if e, isEnv := vs.(envSource); isEnv && e.IsFromEnv() {
				envs = append(envs, e.Key())
				continue
			}
			im.notef(path, imp.Lossy, "%s also reads %v: declare variable_file, or config_files and key:, by hand", what, vs)
		}
	default:
		if e, isEnv := f.(envFlag); isEnv {
			envs = e.GetEnvVars()
		}
	}
	for _, e := range envs {
		if !envName.MatchString(e) {
			im.notef(path, imp.Lossy, "%s reads the environment variable %q, which is not a valid rotini variable name; it was dropped", what, e)
			continue
		}
		s.Variable = append(s.Variable, e)
	}
}

// flagSchema maps the flag's type and default from its Go type: the concrete flag type for
// the few that are not generic, else the type of its Value.
func (im *importer) flagSchema(f cli.Flag, path, what string) *imp.Schema {
	s := &imp.Schema{}
	if bf, ok := f.(*cli.BoolWithInverseFlag); ok {
		s.Type = "bool"
		if bf.Value {
			s.Default = true
		}
		im.inverse(bf, s, path, what)
		return s
	}
	val := field(f, "Value")
	if !val.IsValid() {
		s.Type = "string"
		im.notef(path, imp.Lossy, "%s is a flag of the custom type %T: imported as string; use a type with import: and a TextUnmarshaler, or an enum", what, f)
		return s
	}
	t := val.Type()
	if t.Kind() == reflect.Interface {
		s.Type = "string"
		im.notef(path, imp.Lossy, "%s has the custom value type %s: imported as string; use a type with import: and a TextUnmarshaler, or an enum", what, valueTypeName(val))
		if d, ok := f.(interface{ GetValue() string }); ok && d.GetValue() != "" {
			s.Default = d.GetValue()
		}
		return s
	}
	elem, kind := t, ""
	switch {
	case t.Kind() == reflect.Map && t.Key().Kind() == reflect.String:
		elem, kind = t.Elem(), "map"
	case t.Kind() == reflect.Slice:
		elem, kind = t.Elem(), "slice"
	}
	base, ok := goType(elem)
	if !ok {
		s.Type = "string"
		im.notef(path, imp.Lossy, "%s has the custom type %s: imported as string; use a type with import: and a TextUnmarshaler, or an enum", what, t)
		return s
	}
	if count := field(f, "Config").FieldByName("Count"); base == "bool" && kind == "" && count.IsValid() && !count.IsNil() {
		s.Type = "count"
		return s
	}
	switch kind {
	case "map":
		s.Type = "map[string]" + base
		s.Separator = im.sep
		if sep := fieldString(im.root, "MapFlagKeyValueSeparator"); sep != "" && sep != "=" {
			im.notef(path, imp.Lossy, "%s splits keys from values on %q: rotini splits them on =", what, sep)
		}
	case "slice":
		s.Type = "[]" + base
		s.Separator = im.sep
	default:
		s.Type = base
	}
	im.configNotes(f, base, path, what)
	if !val.IsZero() {
		s.Default = defaultValue(val)
	}
	return s
}

func valueTypeName(v reflect.Value) string {
	if v.IsNil() {
		return v.Type().String()
	}
	return v.Elem().Type().String()
}

// inverse maps a BoolWithInverseFlag: rotini's negatable for the default "no-" prefix, else
// the one negated identifier.
func (im *importer) inverse(bf *cli.BoolWithInverseFlag, s *imp.Schema, path, what string) {
	prefix := bf.InversePrefix
	if prefix == "" || prefix == "no-" {
		s.Negatable = true
		return
	}
	var longs []string
	for _, n := range flagNames(bf) {
		if len(n) > 1 {
			longs = append(longs, "--"+prefix+n)
		}
	}
	if len(longs) == 0 {
		return
	}
	s.Negatable = longs[0]
	if len(longs) > 1 {
		im.notef(path, imp.Lossy, "%s takes the negated forms %v: rotini declares one, %s", what, longs, longs[0])
	}
}

// configNotes reports the type-specific settings rotini has no key for.
func (im *importer) configNotes(f cli.Flag, base, path, what string) {
	cfg := field(f, "Config")
	if !cfg.IsValid() || cfg.Kind() != reflect.Struct {
		return
	}
	if b := cfg.FieldByName("Base"); b.IsValid() && b.CanInt() && b.Int() != 0 && b.Int() != 10 {
		im.notef(path, imp.Lossy, "%s parses numbers in base %d: rotini parses decimal; take a string and parse it in the handler", what, b.Int())
	}
	if base != "time" {
		return
	}
	if l := cfg.FieldByName("Layouts"); l.IsValid() && l.Len() > 0 {
		im.notef(path, imp.Lossy, "%s parses times with the layouts %v: declare them as layout:", what, l.Interface())
	}
	if tz := cfg.FieldByName("Timezone"); tz.IsValid() && !tz.IsNil() {
		im.notef(path, imp.Lossy, "%s reads times in the zone %v: rotini reads a layout with no zone as UTC; convert it in the handler", what, tz.Interface())
	}
}

// goType maps a Go type a urfave/cli flag or argument holds to a rotini type.
func goType(t reflect.Type) (string, bool) {
	switch t {
	case reflect.TypeFor[time.Duration]():
		return "duration", true
	case reflect.TypeFor[time.Time]():
		return "time", true
	}
	switch t.Kind() {
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return t.Kind().String(), true
	}
	return "", false
}

// defaultValue renders a flag's or argument's default as the description holds it: numbers,
// bools and strings as they are, durations and times as text, lists and maps item by item.
func defaultValue(v reflect.Value) any {
	switch v.Type() {
	case reflect.TypeFor[time.Duration]():
		return time.Duration(v.Int()).String()
	case reflect.TypeFor[time.Time]():
		tm, _ := v.Interface().(time.Time)
		return tm.Format(time.RFC3339)
	}
	switch v.Kind() {
	case reflect.Bool:
		return v.Bool()
	case reflect.String:
		return v.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint()
	case reflect.Float32, reflect.Float64:
		return v.Float()
	case reflect.Slice:
		out := make([]any, 0, v.Len())
		for i := range v.Len() {
			out = append(out, defaultValue(v.Index(i)))
		}
		return out
	case reflect.Map:
		out := map[string]any{}
		iter := v.MapRange()
		for iter.Next() {
			out[iter.Key().String()] = defaultValue(iter.Value())
		}
		return out
	}
	return fmt.Sprint(v.Interface())
}

// flagGroups maps c's mutually exclusive groups. A group whose options are single flags is
// rotini's mutually_exclusive, or one_of when urfave/cli requires one of them.
func (im *importer) flagGroups(c *cli.Command, path string, names map[any]string) []*imp.FlagGroup {
	var out []*imp.FlagGroup
	for _, g := range c.MutuallyExclusiveFlags {
		fg := &imp.FlagGroup{Kind: "mutually_exclusive"}
		if g.Required {
			fg.Kind = "one_of"
		}
		single := true
		for _, opt := range g.Flags {
			if len(opt) != 1 {
				single = false
				break
			}
			if n, ok := names[flagKey(opt[0])]; ok {
				fg.Flags = append(fg.Flags, n)
			}
		}
		switch {
		case !single:
			im.notef(path, imp.Unsupported, "a mutually exclusive group has an option of several flags: a rotini group names single flags, so it was dropped; check it in the handler")
		case len(fg.Flags) > 1:
			out = append(out, fg)
		}
	}
	return out
}
