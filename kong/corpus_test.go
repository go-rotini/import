package kong_test

import (
	"fmt"
	"testing"

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
