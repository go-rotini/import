package urfave_test

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
		name, fixture, root, options string
	}{
		{name: "demo", fixture: "demo", root: "Root()"},
		{name: "demo-strict", fixture: "demo", root: "Root()", options: "Strict: true"},
		{name: "readme", fixture: "readme", root: "newApp()"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			corpustest.Check(t, tc.name, tc.fixture, ".", testFile(tc.root, tc.options))
		})
	}
}

// The network corpus imports real programs downloaded at a pinned version, so it runs only
// with ROTINI_IMPORT_NETWORK=1. Their note counts must stay within 10% of the recorded
// baseline; -update rewrites the notes files the baseline was read from.
func TestNetworkCorpus(t *testing.T) {
	corpustest.Network(t)
	cases := []struct {
		name, module, pkg, root string
		baseline                imp.Stats
	}{
		{"woodpecker", "go.woodpecker-ci.org/woodpecker/v3@v3.19.1-0.20261012144008-b9573e7f525c", "./cmd/cli", "newApp()", imp.Stats{Commands: 92, Flags: 302, Lossy: 8, Unsupported: 1, Info: 77}},
		{"gitea", "gitea.dev@v1.28.0-dev.0.20261012161222-0eb0126fa244", "./cmd", "NewMainApp(AppVersion{})", imp.Stats{Commands: 75, Flags: 338, Lossy: 3, Unsupported: 2, Info: 60}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, stderr, err := corpustest.Run(t, corpustest.Download(t, tc.module), tc.pkg, testFile(tc.root, ""))
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
	rotiniimporturfave "github.com/go-rotini/import/urfave"
	rotiniimportos "os"
	rotiniimporttesting "testing"
)

func %[2]s(rotiniImportT *rotiniimporttesting.T) {
	rotiniImportResult, rotiniImportErr := rotiniimporturfave.FromURFave(%[3]s, rotiniimporturfave.Options{%[4]s})
	if rotiniImportErr != nil {
		rotiniImportT.Fatal(rotiniImportErr)
	}
	if rotiniImportErr := rotiniimport.WriteFile(rotiniimportos.Getenv("ROTINI_IMPORT_OUT"), rotiniImportResult); rotiniImportErr != nil {
		rotiniImportT.Fatal(rotiniImportErr)
	}
}
`

// testFile is the test file for the tree the Go expression root yields.
func testFile(root, options string) corpustest.TestFile {
	return func(pkgName, testName string) string {
		return fmt.Sprintf(testTemplate, pkgName, testName, root, options)
	}
}
