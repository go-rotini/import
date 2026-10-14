package cobra_test

import (
	"fmt"
	"path/filepath"
	"strings"
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
		name, pkg, root string
		strict          bool
	}{
		{name: "demo", pkg: ".", root: "Root()"},
		{name: "demo-strict", pkg: ".", root: "Root()", strict: true},
		{name: "cobracli", pkg: "./cmd", root: "rootCmd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			corpustest.Check(t, tc.name, strings.TrimSuffix(tc.name, "-strict"), tc.pkg, testFile(tc.root, tc.strict))
		})
	}
}

// The network corpus imports large real programs pinned in testdata/network. It downloads
// them, so it runs only with ROTINI_IMPORT_NETWORK=1. Their note counts must stay within 10%
// of the recorded baseline; -update rewrites the notes files the baseline was read from.
func TestNetworkCorpus(t *testing.T) {
	corpustest.Network(t)
	cases := []struct {
		name     string
		baseline imp.Stats
	}{
		{"delve", imp.Stats{Commands: 17, Flags: 42, Lossy: 13, Unsupported: 0, Info: 18}},
		{"kubectl", imp.Stats{Commands: 109, Flags: 946, Lossy: 231, Unsupported: 3, Info: 99}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir, err := filepath.Abs(filepath.Join("testdata", "network", tc.name))
			if err != nil {
				t.Fatal(err)
			}
			res, stderr, err := corpustest.Run(t, dir, ".", testFile("Root()", false))
			if err != nil {
				t.Fatalf("import failed: %v\n%s", err, stderr)
			}
			corpustest.Baseline(t, tc.name, res, tc.baseline)
		})
	}
}

// A package that calls flag.Parse in init fails under go test, before any test runs; the
// failure names the -test. flags, which is what a caller recognizes to explain it.
func TestCorpusFlagParseInInit(t *testing.T) {
	corpustest.HasGo(t)
	t.Parallel()
	_, stderr, err := corpustest.Run(t, corpustest.Fixture(t, "flagparse"), ".", testFile("rootCmd", false))
	if err == nil {
		t.Fatal("import succeeded; want the flag.Parse failure")
	}
	if !strings.Contains(stderr, "flag provided but not defined: -test.") {
		t.Fatalf("stderr does not name the -test. flags:\n%s", stderr)
	}
}

// testTemplate is the file the import adds to the program's package. Every identifier it
// declares or imports carries a rotiniImport prefix so it cannot collide with the package's.
const testTemplate = `package %[1]s

import (
	rotiniimport "github.com/go-rotini/import"
	rotiniimportcobra "github.com/go-rotini/import/cobra"
	rotiniimportos "os"
	rotiniimporttesting "testing"
)

func %[2]s(rotiniImportT *rotiniimporttesting.T) {
	rotiniImportResult, rotiniImportErr := rotiniimportcobra.FromCobra((%[3]s).Root(), rotiniimportcobra.Options{Strict: %[4]t})
	if rotiniImportErr != nil {
		rotiniImportT.Fatal(rotiniImportErr)
	}
	if rotiniImportErr := rotiniimport.WriteFile(rotiniimportos.Getenv("ROTINI_IMPORT_OUT"), rotiniImportResult); rotiniImportErr != nil {
		rotiniImportT.Fatal(rotiniImportErr)
	}
}
`

// testFile is the test file for a tree rooted at the Go expression root.
func testFile(root string, strict bool) corpustest.TestFile {
	return func(pkgName, testName string) string {
		return fmt.Sprintf(testTemplate, pkgName, testName, root, strict)
	}
}
