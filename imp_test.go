package imp_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	imp "github.com/go-rotini/import"
)

func sample() *imp.Result {
	two := 2
	once := false
	return &imp.Result{
		Source: imp.Source{Framework: "cobra", Version: "v1.10.2", Importer: "v0.1.0"},
		Command: &imp.Command{
			Name: "acme",
			Flags: []*imp.Flag{
				{Name: "help", Identifiers: []string{"-h", "--help"}, Cascading: true, ShortCircuit: true, Schema: &imp.Schema{Type: "bool"}, Builtin: "help"},
				{Name: "ports", Identifiers: []string{"--ports"}, Schema: &imp.Schema{Type: "[]int", Default: []any{int64(80), int64(443)}, Separator: ","}},
				{Name: "ratio", Identifiers: []string{"--ratio"}, Schema: &imp.Schema{Type: "float64", Default: 0.5}},
				{Name: "set", Identifiers: []string{"--set"}, Schema: &imp.Schema{Type: "map[string]int", Default: map[string]any{"a": int64(1)}}},
				{Name: "big", Identifiers: []string{"--big"}, Schema: &imp.Schema{Type: "uint64", Default: uint64(18446744073709551615)}},
				{Name: "token", Identifiers: []string{"--token"}, Group: "Auth", Schema: &imp.Schema{Type: "string", Variable: []string{"APP_TOKEN", "TOKEN"}, Repeatable: &once}},
				{Name: "color", Identifiers: []string{"--color"}, Schema: &imp.Schema{Type: "bool", Negatable: "--plain"}},
				{Name: "wait", Identifiers: []string{"--wait"}, Schema: &imp.Schema{Type: "bool", Negatable: true}},
			},
			Commands: []*imp.Command{{
				Name:         "deploy",
				OptionsFirst: true,
				Arguments: []*imp.Argument{
					{Name: "svc", Schema: &imp.Schema{Type: "[]string", Required: true, MinItems: &two, Items: &imp.Schema{Type: "string", Enum: []imp.EnumValue{{Value: "api", Summary: "the <api>"}}}}},
					{Name: "rest", Passthrough: true, Schema: &imp.Schema{Type: "[]string"}},
				},
				FlagGroups: []*imp.FlagGroup{{Kind: "one_of", Flags: []string{"a", "b"}}},
			}},
		},
		Notes: []imp.Note{
			{Path: "acme deploy", Level: imp.Lossy, Msg: "x"},
			{Path: "acme", Level: imp.Info, Msg: "y"},
			{Path: "acme", Level: imp.Unsupported, Msg: "z"},
		},
		Hooks: []imp.Hook{{Path: "acme deploy", Framework: "RunE", Rotini: "Run", Func: "main.run", File: "/src/main.go", Line: 12}},
	}
}

func TestWriteRead(t *testing.T) {
	var buf bytes.Buffer
	if err := imp.Write(&buf, sample()); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), "{\n  \"format\": \"rotini-import/1\",") {
		t.Fatalf("output starts %q", buf.String()[:40])
	}
	if !strings.Contains(buf.String(), "the <api>") {
		t.Fatal("HTML escaping is on")
	}
	got, err := imp.Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	want := sample()
	want.Format = imp.Format
	// uint64 beyond int64 decodes as float64.
	want.Command.Flags[4].Schema.Default = float64(18446744073709551615)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip differs:\n got %#v\nwant %#v", got.Command.Flags, want.Command.Flags)
	}
}

func TestWriteErrors(t *testing.T) {
	if err := imp.Write(&bytes.Buffer{}, nil); err == nil {
		t.Fatal("nil result written")
	}
	if err := imp.Write(&bytes.Buffer{}, &imp.Result{}); err == nil {
		t.Fatal("result without a command written")
	}
	if err := imp.Write(failWriter{}, sample()); err == nil {
		t.Fatal("write error lost")
	}
	bad := sample()
	bad.Command.Flags[1].Schema.Default = func() {}
	if err := imp.Write(&bytes.Buffer{}, bad); err == nil {
		t.Fatal("encode error lost")
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestWriteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.json")
	if err := imp.WriteFile(path, sample()); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := imp.Read(f); err != nil {
		t.Fatal(err)
	}
	if err := imp.WriteFile("", sample()); err == nil {
		t.Fatal("empty name accepted")
	}
	if err := imp.WriteFile(filepath.Join(t.TempDir(), "missing", "out.json"), sample()); err == nil {
		t.Fatal("unwritable path accepted")
	}
	if err := imp.WriteFile(path, nil); err == nil {
		t.Fatal("nil result written")
	}
}

func TestReadRefuses(t *testing.T) {
	cases := map[string]string{
		"not json":      "nope",
		"no format":     `{"command":{"name":"x"}}`,
		"other format":  `{"format":"other/1","command":{"name":"x"}}`,
		"next major":    `{"format":"rotini-import/2","command":{"name":"x"}}`,
		"no command":    `{"format":"rotini-import/1"}`,
		"wrong types":   `{"format":"rotini-import/1","command":{"name":3}}`,
		"empty version": `{"format":"rotini-import/","command":{"name":"x"}}`,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := imp.Read(strings.NewReader(in)); err == nil {
				t.Fatal("accepted")
			}
		})
	}
	_, err := imp.Read(strings.NewReader(`{"format":"rotini-import/2","command":{"name":"x"}}`))
	if !errors.Is(err, imp.ErrFormat) {
		t.Fatalf("error %v does not wrap ErrFormat", err)
	}
}

func TestReadAccepts(t *testing.T) {
	in := `{"format":"rotini-import/1.4","future":true,"command":{"name":"x","flags":[{"name":"n","identifiers":["--n"],"schema":{"type":"int","default":3}}]}}`
	r, err := imp.Read(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if d := r.Command.Flags[0].Schema.Default; d != int64(3) {
		t.Fatalf("default %#v", d)
	}
}

func TestStats(t *testing.T) {
	s := sample().Stats()
	want := imp.Stats{Commands: 2, Flags: 8, Lossy: 1, Unsupported: 1, Info: 1}
	if s != want {
		t.Fatalf("stats %+v", s)
	}
	if got := s.String(); got != "imported 2 commands, 8 flags: 1 lossy, 1 unsupported, 1 info" {
		t.Fatalf("summary %q", got)
	}
	if (&imp.Result{}).Stats() != (imp.Stats{}) {
		t.Fatal("empty result counts")
	}
}

func TestNoteString(t *testing.T) {
	n := imp.Note{Path: "acme deploy", Level: imp.Lossy, Msg: "--x is gone"}
	if got := n.String(); got != "[lossy] acme deploy: --x is gone" {
		t.Fatalf("got %q", got)
	}
}
