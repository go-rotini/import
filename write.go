package imp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
)

// ErrFormat reports a description that is not a rotini import description, or whose major
// version this package does not know.
var ErrFormat = errors.New("unsupported import description format")

const formatPrefix = "rotini-import/"

// Write encodes r as indented JSON, setting r.Format to [Format].
func Write(w io.Writer, r *Result) error {
	if r == nil || r.Command == nil {
		return errors.New("imp: nothing to write: the result has no command")
	}
	r.Format = Format
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		return fmt.Errorf("imp: encode: %w", err)
	}
	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("imp: write: %w", err)
	}
	return nil
}

// WriteFile writes r to the named file with [Write], creating or truncating it.
func WriteFile(name string, r *Result) error {
	if name == "" {
		return errors.New("imp: no output file named")
	}
	var buf bytes.Buffer
	if err := Write(&buf, r); err != nil {
		return err
	}
	if err := os.WriteFile(name, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("imp: %w", err)
	}
	return nil
}

// Read decodes a description written by [Write]. It fails with an error wrapping
// [ErrFormat] when the description's format is missing or has another major version.
// Unknown fields are ignored. A numeric default decodes as int64 when it is a whole number
// that fits, and as float64 otherwise.
func Read(rd io.Reader) (*Result, error) {
	dec := json.NewDecoder(rd)
	dec.UseNumber()
	var r Result
	if err := dec.Decode(&r); err != nil {
		return nil, fmt.Errorf("imp: decode: %w", err)
	}
	if err := checkFormat(r.Format); err != nil {
		return nil, err
	}
	if r.Command == nil {
		return nil, errors.New("imp: the description has no command")
	}
	normalizeCommand(r.Command)
	return &r, nil
}

func checkFormat(f string) error {
	rest, ok := strings.CutPrefix(f, formatPrefix)
	if !ok {
		return fmt.Errorf("%w: %q is not %q", ErrFormat, f, Format)
	}
	major, _, _ := strings.Cut(rest, ".")
	if major != strings.TrimPrefix(Format, formatPrefix) {
		return fmt.Errorf("%w: %q has major version %s; this reader knows %q", ErrFormat, f, major, Format)
	}
	return nil
}

func normalizeCommand(c *Command) {
	for _, a := range c.Arguments {
		normalizeSchema(a.Schema)
	}
	for _, f := range c.Flags {
		normalizeSchema(f.Schema)
	}
	for _, sub := range c.Commands {
		normalizeCommand(sub)
	}
}

func normalizeSchema(s *Schema) {
	if s == nil {
		return
	}
	s.Default = normalizeValue(s.Default)
	normalizeSchema(s.Items)
}

func normalizeValue(v any) any {
	switch t := v.(type) {
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return n
		}
		if f, err := t.Float64(); err == nil && !math.IsInf(f, 0) {
			return f
		}
		return t.String()
	case []any:
		for i := range t {
			t[i] = normalizeValue(t[i])
		}
	case map[string]any:
		for k := range t {
			t[k] = normalizeValue(t[k])
		}
	}
	return v
}
