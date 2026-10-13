package kong_test

import (
	"fmt"
	"testing"

	imp "github.com/go-rotini/import"
	"github.com/go-rotini/import/internal/corpustest"
)

// The corpus runs the import the way the rotini companion CLI does: a temporary modfile
// outside the program's module, a test file added through -overlay with the program's own
// test files blanked, and the description read back from a file. Each fixture is a module of
// its own under testdata/corpus, and none of its files may change.
func TestCorpus(t *testing.T) {
	corpustest.HasGo(t)
	cases := []struct {
		name, fixture, grammar, options string
	}{
		{name: "demo", fixture: "demo", grammar: "&CLI{}", options: `Name: "acme", Vars: map[string]string{"stage": "staging"}`},
		{name: "readme", fixture: "readme", grammar: "&CLI"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			corpustest.Check(t, tc.name, tc.fixture, ".", testFile(tc.grammar, tc.options))
		})
	}
}

// The network corpus imports real programs downloaded at a pinned version, so it runs only
// with ROTINI_IMPORT_NETWORK=1. Their note counts must stay within 10% of the recorded
// baseline; -update rewrites the notes files the baseline was read from. git-spice builds
// against a fork of Kong that allows negatable on bool-like values that are not bools.
func TestNetworkCorpus(t *testing.T) {
	corpustest.Network(t)
	cases := []struct {
		name, module, pkg, grammar, options string
		baseline                            imp.Stats
	}{
		{"git-spice", "go.abhg.dev/gs@v0.31.3-0.20261001180930-c5274c7df6e6", ".", "&mainCmd{}", `Name: "gs"`, imp.Stats{Commands: 74, Flags: 302, Lossy: 71, Unsupported: 11, Info: 3}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, stderr, err := corpustest.Run(t, corpustest.Download(t, tc.module), tc.pkg, testFile(tc.grammar, tc.options))
			if err != nil {
				t.Fatalf("import failed: %v\n%s", err, stderr)
			}
			corpustest.Baseline(t, tc.name, res, tc.baseline)
		})
	}
}

// testTemplate is the file the import adds to the program's package.
const testTemplate = `package %[1]s

import (
	rotiniimport "github.com/go-rotini/import"
	rotiniimportkong "github.com/go-rotini/import/kong"
	rotiniimportos "os"
	rotiniimporttesting "testing"
)

func %[2]s(rotiniImportT *rotiniimporttesting.T) {
	rotiniImportResult, rotiniImportErr := rotiniimportkong.FromKong(%[3]s, rotiniimportkong.Options{%[4]s})
	if rotiniImportErr != nil {
		rotiniImportT.Fatal(rotiniImportErr)
	}
	if rotiniImportErr := rotiniimport.WriteFile(rotiniimportos.Getenv("ROTINI_IMPORT_OUT"), rotiniImportResult); rotiniImportErr != nil {
		rotiniImportT.Fatal(rotiniImportErr)
	}
}
`

// testFile is the test file for the grammar the Go expression grammar yields.
func testFile(grammar, options string) corpustest.TestFile {
	return func(pkgName, testName string) string {
		return fmt.Sprintf(testTemplate, pkgName, testName, grammar, options)
	}
}
