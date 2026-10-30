package cobra

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	imp "github.com/go-rotini/import"
	"github.com/go-rotini/import/internal/walk"
	"github.com/spf13/cobra"
)

// probeLimit is the most positionals an Args validator is probed with one count at a time.
// A validator that accepts this many is probed further, up to unboundedProbe.
const probeLimit = 8

// unboundedProbe is the count a validator that accepts probeLimit positionals is tried with:
// one that accepts it too is taken to be unbounded.
const unboundedProbe = 1000

// probe calls the Args validator with 0..probeLimit placeholder arguments and returns the
// counts it accepts. When it accepts probeLimit, most is the largest count beyond it that the
// validator accepts, found by bisection up to unboundedProbe, or -1 when it accepts
// unboundedProbe; otherwise most is the last accepted count. A stock validator that accepts
// none of 0..probeLimit is tried with more, up to unboundedProbe, from the first count it
// accepts. A validator that checks values against ValidArgs is given ValidArgs[0].
func probe(c *cobra.Command, stock bool) (accepted []int, most int) {
	fill := "x"
	if len(c.ValidArgs) > 0 {
		fill = strings.SplitN(c.ValidArgs[0], "\t", 2)[0]
	}
	try := func(n int) bool { return accepts(c, repeat(fill, n)) }
	for n := 0; n <= probeLimit; n++ {
		if try(n) {
			accepted = append(accepted, n)
		}
	}
	for n := probeLimit + 1; stock && len(accepted) == 0 && n <= unboundedProbe; n++ {
		if try(n) {
			accepted = append(accepted, n)
		}
	}
	if len(accepted) == 0 {
		return nil, 0
	}
	most = accepted[len(accepted)-1]
	if most > probeLimit && !try(most+1) {
		return accepted, most
	}
	if most < probeLimit {
		return accepted, most
	}
	if try(unboundedProbe) {
		return accepted, -1
	}
	lo, hi := most, unboundedProbe // lo is accepted, hi is not
	for hi-lo > 1 {
		if mid := (lo + hi) / 2; try(mid) {
			lo = mid
		} else {
			hi = mid
		}
	}
	return accepted, lo
}

// repeat returns n copies of v.
func repeat(v string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = v
	}
	return out
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
	use := c.Use
	if !c.HasParent() {
		use = walk.CutProgram(use)
	}
	declared := walk.ParseUse(use)
	if c.Args == nil {
		if c.HasSubCommands() && !c.HasParent() {
			return nil // Cobra's legacy check rejects a root's positionals that name no sub-command
		}
		return im.fromUse(c, path, declared)
	}
	lo, hi := im.counts(c, path)
	if hi == 0 {
		return nil
	}
	name := func(i int, variadic bool) string {
		switch {
		case i < len(declared):
			return declared[i].Name
		case variadic:
			return "args"
		}
		return fmt.Sprintf("arg%d", i+1)
	}
	// A bound beyond probeLimit is a list with a maximum rather than that many scalars.
	list := hi < 0 || hi > probeLimit
	scalars := hi
	if list {
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
		out = append(out, &imp.Argument{Name: name(i, false), Schema: s})
	}
	if list {
		s := &imp.Schema{Type: "[]string"}
		if rest := lo - scalars; rest > 0 {
			s.Required = true
			s.MinItems = &rest
		}
		if hi > 0 {
			most := hi - scalars
			s.MaxItems = &most
		}
		out = append(out, &imp.Argument{Name: name(scalars, true), Schema: s})
	}
	walk.UniqueNames(out)
	if len(out) > len(declared) {
		im.notef(path, imp.Lossy, "argument names invented (%s): the Use line %q does not name them", argNames(out[len(declared):]), c.Use)
	}
	return out
}

// counts finds how many positionals the Args validator accepts. hi is -1 when unbounded.
func (im *importer) counts(c *cobra.Command, path string) (lo, hi int) {
	stock := isCobraFunc(c.Args)
	acc, most := probe(c, stock)
	switch {
	case len(acc) == 0:
		im.notef(path, imp.Lossy, "Args validator %s accepted none of the probes; imported as any number of arguments, move its checks to the handler", walk.FuncName(c.Args))
		return 0, -1
	case !contiguous(acc):
		im.notef(path, imp.Unsupported, "Args validator %s accepts %v positionals, not a range; imported as any number, move the check to the handler", walk.FuncName(c.Args), acc)
		return 0, -1
	}
	lo, hi = acc[0], most
	if !stock {
		im.notef(path, imp.Lossy, "custom Args validator %s: the count %d..%s was found by probing; move any value checks to the handler", walk.FuncName(c.Args), lo, bound(hi))
	}
	return lo, hi
}

// fromUse builds the arguments of a command with no Args validator from its Use line.
// Cobra accepts any positionals then, so unless Options.Strict a trailing variadic "args"
// keeps that behavior.
func (im *importer) fromUse(c *cobra.Command, path string, declared []walk.UseArg) []*imp.Argument {
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
		out = append(out, &imp.Argument{Name: d.Name, Schema: s})
		if d.Choice {
			im.notef(path, imp.Lossy, "<%s> stands for a choice of alternatives in %q: any one of them fills it, so it was made optional; the handler must tell them apart", d.Name, c.Use)
		}
		if variadic {
			if rest := declared[i+1:]; len(rest) > 0 {
				im.notef(path, imp.Lossy, "%s after the variadic <%s> in %q dropped: a variadic argument is the last; the handler must take them from its end", walk.UseNames(rest), d.Name, c.Use)
			}
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
		out = append(out, &imp.Argument{Name: "args", Summary: "further arguments", Schema: s})
		im.notef(path, imp.Info, "Args unset: Cobra accepts any positionals, so a trailing variadic args was added; delete it to reject extras")
	}
	walk.UniqueNames(out)
	return out
}

// argValues puts ValidArgs and ArgAliases on the first argument, the one Cobra completes them
// for. ValidArgs is an enum when the Args validator rejects a value it does not list, as
// OnlyValidArgs does; otherwise Cobra accepts any value, and ValidArgs is only what it
// completes. ArgAliases, which Cobra completes when no ValidArgs value matches and which
// OnlyValidArgs rejects, are completion values.
func (im *importer) argValues(c *cobra.Command, path string, args []*imp.Argument) {
	valid, aliases := enumValues(c.ValidArgs), enumValues(c.ArgAliases)
	switch {
	case valid == nil && aliases == nil:
		return
	case valid == nil:
		im.notef(path, imp.Info, "ArgAliases %v dropped: Cobra completes them only on a command with ValidArgs", values(aliases))
		return
	case len(args) == 0 && aliases != nil:
		im.notef(path, imp.Info, "ValidArgs %v and ArgAliases %v dropped: the command takes no arguments", values(valid), values(aliases))
		return
	case len(args) == 0:
		im.notef(path, imp.Info, "ValidArgs %v dropped: the command takes no arguments", values(valid))
		return
	}
	first := args[0]
	s := first.Schema
	if enforcesValidArgs(c) {
		if s.Type == "[]string" {
			s.Items = &imp.Schema{Type: "string", Enum: valid}
		} else {
			s.Enum = valid
		}
		if len(args) > 1 {
			im.notef(path, imp.Lossy, "ValidArgs became the enum of the first argument only")
		}
	} else {
		s.Completions = valid
		im.notef(path, imp.Info, "ValidArgs %v imported as completion values of <%s>, not an enum: the Args validator does not check values against them, so Cobra accepts any value", values(valid), first.Name)
	}
	for _, a := range aliases {
		if !slices.ContainsFunc(s.Completions, func(v imp.EnumValue) bool { return v.Value == a.Value }) {
			s.Completions = append(s.Completions, a)
		}
	}
	if aliases != nil {
		im.notef(path, imp.Info, "ArgAliases %v imported as completion values of <%s>: Cobra offers them only when no ValidArgs value matches the word", values(aliases), first.Name)
	}
}

// enforcesValidArgs reports whether c's Args validator checks values against ValidArgs: at
// the first count it accepts ValidArgs' first value at, it rejects a value ValidArgs does not
// list.
func enforcesValidArgs(c *cobra.Command) bool {
	if c.Args == nil || len(c.ValidArgs) == 0 {
		return false
	}
	listed := values(enumValues(c.ValidArgs))
	other := "rotini-import-unlisted"
	for slices.Contains(listed, other) {
		other += "_"
	}
	for n := 1; n <= probeLimit; n++ {
		if accepts(c, repeat(listed[0], n)) {
			return !accepts(c, repeat(other, n))
		}
	}
	return false
}

// enumValues reads Cobra's "value" or "value\tdescription" words as enum values.
func enumValues(words []string) []imp.EnumValue {
	if len(words) == 0 {
		return nil
	}
	out := make([]imp.EnumValue, 0, len(words))
	for _, v := range words {
		value, summary, _ := strings.Cut(v, "\t")
		out = append(out, imp.EnumValue{Value: value, Summary: strings.TrimSpace(summary)})
	}
	return out
}

func values(es []imp.EnumValue) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.Value)
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
