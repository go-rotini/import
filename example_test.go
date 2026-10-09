package imp_test

import (
	"bytes"
	"fmt"

	imp "github.com/go-rotini/import"
)

func ExampleRead() {
	res := &imp.Result{
		Source:  imp.Source{Framework: "cobra"},
		Command: &imp.Command{Name: "acme"},
		Notes:   []imp.Note{{Path: "acme", Level: imp.Info, Msg: "commands are imported in alphabetical order"}},
	}
	var buf bytes.Buffer
	if err := imp.Write(&buf, res); err != nil {
		fmt.Println(err)
		return
	}
	got, err := imp.Read(&buf)
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, n := range got.Notes {
		fmt.Println(n)
	}
	fmt.Println(got.Stats())
	// Output:
	// [info] acme: commands are imported in alphabetical order
	// imported 1 commands, 0 flags: 0 lossy, 0 unsupported, 1 info
}
