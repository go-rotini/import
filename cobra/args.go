package cobra

import (
	"fmt"
	"strconv"
	"strings"

	imp "github.com/go-rotini/import"
	"github.com/go-rotini/import/internal/walk"
	"github.com/spf13/cobra"
)

// probeLimit is the most positionals an Args validator is probed with. A validator that
// accepts this many is taken to be unbounded.
const probeLimit = 8

// probe calls the Args validator with 0..probeLimit placeholder arguments and returns the
// counts it accepts. A validator that checks values against ValidArgs is given ValidArgs[0].
func probe(c *cobra.Command) []int {
	fill := "x"
	if len(c.ValidArgs) > 0 {
		fill = strings.SplitN(c.ValidArgs[0], "\t", 2)[0]
	}
	var accepted []int
	for n := 0; n <= probeLimit; n++ {
		args := make([]string, n)
		for i := range args {
			args[i] = fill
		}
		if accepts(c, args) {
			accepted = append(accepted, n)
		}
	}
	return accepted
}

func accepts(c *cobra.Command, args []string) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	return c.Args(c, args) == nil
}

func contiguous(counts []int) bool {
	for i := 1; i < len(counts); i++ {
		if counts[i] != counts[i-1]+1 {
			return false
		}
	}
	return true
}

func (im *importer) arguments(c *cobra.Command, path string) []*imp.Argument {
	if !c.Runnable() {
		return nil // Cobra shows help for a command it cannot run, whatever the words
	}
	declared := walk.ParseUse(c.Use)
	if c.Args == nil {
		if c.HasSubCommands() {
			return nil
		}
		return im.fromUse(c, path, declared)
	}
	lo, hi := im.counts(c, path)
	if hi == 0 {
		return nil
	}
	enum := validArgs(c)
	invented := false
	name := func(i int, variadic bool) string {
		if i < len(declared) {
			return declared[i].Name
		}
		invented = true
		if variadic {
			return "args"
		}
		return fmt.Sprintf("arg%d", i+1)
	}
	scalars := hi
	if hi < 0 {
		scalars = lo
		// Keep the required ones as named scalars only when Use names each of them and the
		// last is not itself the variadic one.
		if lo > 0 && (len(declared) < lo || declared[lo-1].Variadic) {
			scalars = 0
		}
	}
	var out []*imp.Argument
	for i := range scalars {
		s := &imp.Schema{Type: "string", Required: i < lo}
		if i == 0 {
			s.Enum = enum
		}
		out = append(out, &imp.Argument{Name: name(i, false), Schema: s})
	}
	if hi < 0 {
		s := &imp.Schema{Type: "[]string"}
		if rest := lo - scalars; rest > 0 {
			s.Required = true
			s.MinItems = &rest
		}
		if scalars == 0 && enum != nil {
			s.Items = &imp.Schema{Type: "string", Enum: enum}
		}
		out = append(out, &imp.Argument{Name: name(scalars, true), Schema: s})
	}
	walk.UniqueNames(out)
	if enum != nil && len(out) > 1 {
		im.notef(path, imp.Lossy, "ValidArgs became the enum of the first argument only")
	}
	if invented {
		im.notef(path, imp.Lossy, "argument names invented (%s): the Use line %q does not name them", argNames(out), c.Use)
	}
	return out
}

// counts finds how many positionals the Args validator accepts. hi is -1 when unbounded.
func (im *importer) counts(c *cobra.Command, path string) (lo, hi int) {
	acc := probe(c)
	stock := isCobraFunc(c.Args)
	switch {
	case len(acc) == 0:
		im.notef(path, imp.Lossy, "Args validator %s accepted none of the probes; imported as any number of arguments, move its checks to the handler", walk.FuncName(c.Args))
		return 0, -1
	case !contiguous(acc):
		im.notef(path, imp.Unsupported, "Args validator %s accepts %v positionals, not a range; imported as any number, move the check to the handler", walk.FuncName(c.Args), acc)
		return 0, -1
	}
	lo, hi = acc[0], acc[len(acc)-1]
	if hi == probeLimit {
		hi = -1
	}
	if !stock {
		im.notef(path, imp.Lossy, "custom Args validator %s: the count %d..%s was found by probing; move any value checks to the handler", walk.FuncName(c.Args), lo, bound(hi))
	}
	return lo, hi
}

// fromUse builds the arguments of a command with no Args validator from its Use line.
// Cobra accepts any positionals then, so unless Options.Strict a trailing variadic "args"
// keeps that behavior.
func (im *importer) fromUse(c *cobra.Command, path string, declared []walk.UseArg) []*imp.Argument {
	enum := validArgs(c)
	var out []*imp.Argument
	variadic := false
	for i, d := range declared {
		s := &imp.Schema{Type: "string", Required: !d.Optional}
		if d.Variadic {
			s.Type, variadic = "[]string", true
			if !d.Optional {
				one := 1
				s.MinItems = &one
			}
		}
		if i == 0 && enum != nil {
			if d.Variadic {
				s.Items = &imp.Schema{Type: "string", Enum: enum}
			} else {
				s.Enum = enum
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
			im.notef(path, imp.Lossy, "<%s> is required after an optional placeholder in %q: arguments fill in order, so it was made optional; the handler must check which were given", a.Name, c.Use)
		}
	}
	if !variadic && !im.opts.Strict {
		s := &imp.Schema{Type: "[]string"}
		if len(out) == 0 && enum != nil {
			s.Items = &imp.Schema{Type: "string", Enum: enum}
		}
		out = append(out, &imp.Argument{Name: "args", Summary: "further arguments", Schema: s})
		im.notef(path, imp.Info, "Args unset: Cobra accepts any positionals, so a trailing variadic args was added; delete it to reject extras")
	}
	walk.UniqueNames(out)
	return out
}

// validArgs reads ValidArgs ("value" or "value\tdescription") as enum values.
func validArgs(c *cobra.Command) []imp.EnumValue {
	if len(c.ValidArgs) == 0 {
		return nil
	}
	out := make([]imp.EnumValue, 0, len(c.ValidArgs))
	for _, v := range c.ValidArgs {
		value, summary, _ := strings.Cut(v, "\t")
		out = append(out, imp.EnumValue{Value: value, Summary: strings.TrimSpace(summary)})
	}
	return out
}

func bound(n int) string {
	if n < 0 {
		return "any"
	}
	return strconv.Itoa(n)
}

func argNames(as []*imp.Argument) string {
	ns := make([]string, 0, len(as))
	for _, a := range as {
		ns = append(ns, a.Name)
	}
	return strings.Join(ns, ", ")
}
