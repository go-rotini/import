package kong

import (
	"reflect"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	imp "github.com/go-rotini/import"
	"github.com/go-rotini/import/internal/walk"
)

// A negatable non-bool flag is built by hand: Kong releases that reject one at kong.New
// cannot build it, while forks and newer releases can.
func TestNegatableNonBool(t *testing.T) {
	restack := ""
	tag := &kong.Tag{}
	switch neg := reflect.ValueOf(tag).Elem().FieldByName("Negatable"); neg.Kind() {
	case reflect.Bool:
		neg.SetBool(true)
	case reflect.String:
		neg.SetString("_")
	default:
		t.Skip("this Kong has no negatable flags")
	}
	f := &kong.Flag{Value: &kong.Value{
		Name:   "restack",
		Target: reflect.ValueOf(&restack).Elem(),
		Tag:    tag,
	}}
	im := &importer{orig: map[*imp.Flag]string{}}
	rf, negated := im.flag(f, "app", false, walk.Names{})
	if rf.Schema.Negatable != nil {
		t.Fatalf("--restack kept negatable: %+v", rf.Schema)
	}
	if negated == nil || negated.Name != "no-restack" || negated.Schema.Type != "bool" ||
		!reflect.DeepEqual(negated.Identifiers, []string{"--no-restack"}) {
		t.Fatalf("negated flag %+v", negated)
	}
	if len(im.notes) != 1 || im.notes[0].Level != imp.Lossy || !strings.Contains(im.notes[0].Msg, "--restack is negatable but its type is string") {
		t.Fatalf("notes %v", im.notes)
	}

	// The negated form is listed after the command's own flags.
	im = &importer{app: &kong.Kong{Model: &kong.Application{Node: &kong.Node{}}}, groupNodes: map[string]int{}, orig: map[*imp.Flag]string{}}
	flags, _ := im.flags(&kong.Node{Flags: []*kong.Flag{f}}, "app")
	if len(flags) != 2 || flags[0].Name != "restack" || flags[1].Name != "no-restack" {
		t.Fatalf("flags %+v", flags)
	}
}
