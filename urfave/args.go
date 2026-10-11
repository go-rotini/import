package urfave

import (
	"fmt"

	imp "github.com/go-rotini/import"
	"github.com/go-rotini/import/internal/walk"
	"github.com/urfave/cli/v3"
)

func (im *importer) arguments(c *cli.Command, path string, hasSubs bool) []*imp.Argument {
	if len(c.Arguments) > 0 {
		return im.declared(c, path)
	}
	if hasSubs {
		return nil
	}
	return im.fromUsage(c, path)
}

// declared maps c's Arguments. An argument that takes more than one value is a list, with
// its minimum and maximum counts.
func (im *importer) declared(c *cli.Command, path string) []*imp.Argument {
	var out []*imp.Argument
	for i, a := range c.Arguments {
		name := walk.ArgName(fieldString(a, "Name"))
		if name == "" {
			name = fmt.Sprintf("arg%d", i+1)
		}
		what := "<" + name + ">"
		val := field(a, "Value")
		if !val.IsValid() {
			im.notef(path, imp.Lossy, "%s is an argument of the custom type %T: imported as string", what, a)
			out = append(out, &imp.Argument{Name: name, Schema: &imp.Schema{Type: "string"}})
			continue
		}
		base, ok := goType(val.Type())
		if !ok {
			base = "string"
			im.notef(path, imp.Lossy, "%s has the custom type %s: imported as string; use a type with import: and a TextUnmarshaler, or an enum", what, val.Type())
		}
		lo, hi, counted := argCounts(a)
		s := &imp.Schema{Type: base}
		switch {
		case !counted:
			s.Required = fieldBool(a, "Required")
		case hi == 0:
			im.notef(path, imp.Unsupported, "%s takes at most 0 values, so urfave/cli never parses it; it was dropped", what)
			continue
		case hi == 1:
			s.Required = lo >= 1
		default:
			s.Type = "[]" + base
			if lo > 0 {
				s.Required, s.MinItems = true, &lo
			}
			if hi > 1 {
				s.MaxItems = &hi
			}
		}
		if s.Type == base && !val.IsZero() {
			s.Default = defaultValue(val)
		}
		out = append(out, &imp.Argument{Name: name, Schema: s})
	}
	walk.UniqueNames(out)
	return out
}

// argCounts reads an argument's Min and Max. counted is false for the single-value argument
// type of newer releases, which has neither.
func argCounts(a cli.Argument) (lo, hi int, counted bool) {
	mn, mx := field(a, "Min"), field(a, "Max")
	if !mn.IsValid() || !mx.IsValid() || !mn.CanInt() || !mx.CanInt() {
		return 0, 0, false
	}
	lo, hi = int(mn.Int()), int(mx.Int())
	if hi < 0 {
		hi = -1
	}
	return lo, hi, true
}

// fromUsage builds the arguments of a command that declares none from its ArgsUsage.
// urfave/cli accepts any positionals then, so unless Options.Strict a trailing variadic
// "args" keeps that behavior.
func (im *importer) fromUsage(c *cli.Command, path string) []*imp.Argument {
	declared := walk.ParseArgsUsage(c.ArgsUsage)
	var out []*imp.Argument
	variadic := false
	for _, d := range declared {
		s := &imp.Schema{Type: "string", Required: !d.Optional}
		if d.Variadic {
			s.Type, variadic = "[]string", true
			if !d.Optional {
				one := 1
				s.MinItems = &one
			}
		}
		out = append(out, &imp.Argument{Name: d.Name, Schema: s})
		if variadic {
			break
		}
	}
	walk.UniqueNames(out)
	optionalSeen := false
	for _, a := range out {
		switch {
		case !a.Schema.Required:
			optionalSeen = true
		case optionalSeen:
			a.Schema.Required, a.Schema.MinItems = false, nil
			im.notef(path, imp.Lossy, "<%s> is required after an optional placeholder in %q: arguments fill in order, so it was made optional; the handler must check which were given", a.Name, c.ArgsUsage)
		}
	}
	if !variadic && !im.opts.Strict {
		out = append(out, &imp.Argument{Name: "args", Summary: "further arguments", Schema: &imp.Schema{Type: "[]string"}})
		im.notef(path, imp.Info, "no Arguments declared: urfave/cli accepts any positionals, so a trailing variadic args was added; delete it to reject extras")
		walk.UniqueNames(out)
	}
	return out
}
