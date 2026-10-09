package imp_test

import (
	"bytes"
	"strings"
	"testing"

	imp "github.com/go-rotini/import"
)

// FuzzRead checks that Read never panics, and that what it accepts writes back and reads
// again.
func FuzzRead(f *testing.F) {
	var buf bytes.Buffer
	if err := imp.Write(&buf, sample()); err != nil {
		f.Fatal(err)
	}
	f.Add(buf.String())
	f.Add(`{"format":"rotini-import/1","command":{"name":"x","flags":[{"name":"n","identifiers":["--n"],"schema":{"type":"int","default":[1,{"a":2.5}]}}]}}`)
	f.Add(`{"format":"rotini-import/9"}`)
	f.Fuzz(func(t *testing.T, in string) {
		r, err := imp.Read(strings.NewReader(in))
		if err != nil {
			return
		}
		var out bytes.Buffer
		if err := imp.Write(&out, r); err != nil {
			t.Fatalf("accepted input does not write back: %v", err)
		}
		if _, err := imp.Read(&out); err != nil {
			t.Fatalf("written description does not read back: %v", err)
		}
	})
}
