package kong

import (
	"reflect"

	"github.com/alecthomas/kong"
	imp "github.com/go-rotini/import"
	"github.com/go-rotini/import/internal/walk"
)

// passThroughModePartial is kong.PassThroughModePartial, written out so the importer builds
// against Kong releases that predate passthrough modes.
const passThroughModePartial = 2

// arguments maps n's positionals. A slice or map positional takes the rest of the words.
func (im *importer) arguments(n *kong.Node, path string) []*imp.Argument {
	out := make([]*imp.Argument, 0, len(n.Positional))
	for i, p := range n.Positional {
		name := walk.ArgName(p.Name)
		if name == "" {
			name = "arg"
		}
		what := "<" + name + ">"
		a := &imp.Argument{Name: name, Summary: text(p.Help), Schema: im.schema(p, path, what, false)}
		a.Schema.Placeholder = ""
		if p.Passthrough {
			a.Passthrough = true
			a.Schema.Separator = ""
			if passthroughMode(p) == passThroughModePartial && i > 0 {
				im.notef(path, imp.Lossy, "%s passes words through from the first positional in Kong's partial mode; rotini passes them through from this argument on", what)
			}
		}
		im.valueHooks(p, path, what)
		out = append(out, a)
	}
	walk.UniqueNames(out)
	return out
}

// passthroughMode reads a positional's passthrough mode; Kong releases before 1.8 have only
// the one mode.
func passthroughMode(p *kong.Value) int64 {
	v := reflect.ValueOf(p).Elem().FieldByName("PassthroughMode")
	if !v.IsValid() || !v.CanInt() {
		return 0
	}
	return v.Int()
}
