package cobra_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	imp "github.com/go-rotini/import"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// The corpus runs the import the way the rotini companion CLI does: a temporary modfile
// outside the program's module, a test file added through -overlay with the program's own
// test files blanked, and the description read back from a file. Each fixture is a module of
// its own under testdata/corpus, and none of its files may change.
func TestCorpus(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go command")
	}
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
			dir := fixtureDir(t, strings.TrimSuffix(tc.name, "-strict"))
			before := snapshot(t, dir)
			res, stderr, err := runImport(t, dir, tc.pkg, tc.root, tc.strict)
			if err != nil {
				t.Fatalf("import failed: %v\n%s", err, stderr)
			}
			if after := snapshot(t, dir); after != before {
				t.Fatalf("the import changed the fixture's files")
			}
			if res.Source.Importer == "" {
				t.Fatal("the description does not name the importer's version")
			}
			normalize(res, dir)
			var js bytes.Buffer
			if err := imp.Write(&js, res); err != nil {
				t.Fatal(err)
			}
			golden(t, tc.name+".json", js.Bytes())
			golden(t, tc.name+".notes.txt", []byte(notesText(res)))
		})
	}
}

// The network corpus imports large real programs pinned in testdata/network. It downloads
// them, so it runs only with ROTINI_IMPORT_NETWORK=1. Their note counts must stay within 10%
// of the recorded baseline; -update rewrites the notes files the baseline was read from.
func TestNetworkCorpus(t *testing.T) {
	if os.Getenv("ROTINI_IMPORT_NETWORK") != "1" {
		t.Skip("set ROTINI_IMPORT_NETWORK=1 to import the pinned real programs")
	}
	cases := []struct {
		name     string
		baseline imp.Stats
	}{
		{"delve", imp.Stats{Commands: 17, Flags: 42, Lossy: 13, Unsupported: 0, Info: 17}},
		{"kubectl", imp.Stats{Commands: 109, Flags: 946, Lossy: 105, Unsupported: 3, Info: 92}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir, err := filepath.Abs(filepath.Join("testdata", "network", tc.name))
			if err != nil {
				t.Fatal(err)
			}
			res, stderr, err := runImport(t, dir, ".", "Root()", false)
			if err != nil {
				t.Fatalf("import failed: %v\n%s", err, stderr)
			}
			if *update {
				golden(t, "network-"+tc.name+".notes.txt", []byte(notesText(res)))
			}
			got := res.Stats()
			for _, m := range []struct {
				what      string
				got, want int
			}{
				{"commands", got.Commands, tc.baseline.Commands},
				{"flags", got.Flags, tc.baseline.Flags},
				{"lossy notes", got.Lossy, tc.baseline.Lossy},
				{"unsupported notes", got.Unsupported, tc.baseline.Unsupported},
				{"info notes", got.Info, tc.baseline.Info},
			} {
				if tol := (m.want + 9) / 10; m.got < m.want-tol || m.got > m.want+tol {
					t.Errorf("%s: %d, baseline %d (±%d)", m.what, m.got, m.want, tol)
				}
			}
		})
	}
}

// A package that calls flag.Parse in init fails under go test, before any test runs; the
// failure names the -test. flags, which is what a caller recognizes to explain it.
func TestCorpusFlagParseInInit(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go command")
	}
	t.Parallel()
	_, stderr, err := runImport(t, fixtureDir(t, "flagparse"), ".", "rootCmd", false)
	if err == nil {
		t.Fatal("import succeeded; want the flag.Parse failure")
	}
	if !strings.Contains(stderr, "flag provided but not defined: -test.") {
		t.Fatalf("stderr does not name the -test. flags:\n%s", stderr)
	}
}

func fixtureDir(t *testing.T, name string) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("testdata", "corpus", name))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// runImport imports the package pkg of the module at dir, rooted at the Go expression root.
func runImport(t *testing.T, dir, pkg, root string, strict bool) (*imp.Result, string, error) {
	t.Helper()
	work := t.TempDir()
	shim, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	modfile := filepath.Join(work, "import.mod")
	copyFile(t, filepath.Join(dir, "go.mod"), modfile)
	copyFile(t, filepath.Join(dir, "go.sum"), filepath.Join(work, "import.sum"))

	// The companion runs `go get -modfile=<modfile> github.com/go-rotini/import/cobra@<v>`.
	// The corpus points the modfile at this checkout instead.
	goCmd(t, dir, nil, "mod", "edit", "-modfile="+modfile,
		"-require=github.com/go-rotini/import@v0.0.0",
		"-replace=github.com/go-rotini/import="+filepath.Dir(shim))

	listed := goCmd(t, dir, nil, "list", "-modfile="+modfile, "-mod=mod", "-f", "{{.Name}}\n{{.Dir}}", pkg)
	pkgName, pkgDir, ok := strings.Cut(strings.TrimSpace(listed), "\n")
	if !ok {
		t.Fatalf("go list %s: unexpected output %q", pkg, listed)
	}
	pkgDir = strings.TrimSpace(pkgDir)
	testName := "TestRotiniImport_" + randomSuffix(t)
	testFile := filepath.Join(work, "zz_rotini_import_test.go")
	src := fmt.Sprintf(testTemplate, pkgName, testName, root, strict)
	if err := os.WriteFile(testFile, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	overlay := map[string]string{filepath.Join(pkgDir, "zz_rotini_import_test.go"): testFile}
	existing, err := filepath.Glob(filepath.Join(pkgDir, "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range existing {
		overlay[f] = ""
	}
	ov, err := json.Marshal(map[string]any{"Replace": overlay})
	if err != nil {
		t.Fatal(err)
	}
	overlayFile := filepath.Join(work, "overlay.json")
	if err := os.WriteFile(overlayFile, ov, 0o600); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(work, "out.json")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "-modfile", modfile, "-mod=mod", "-overlay", overlayFile,
		"-count=1", "-run", "^"+testName+"$", "-timeout", "2m", pkg)
	cmd.Dir = dir
	cmd.Env = append(goEnv(), "ROTINI_IMPORT_OUT="+out)
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stderr, &stderr
	if err := cmd.Run(); err != nil {
		return nil, stderr.String(), err
	}
	f, err := os.Open(out)
	if err != nil {
		return nil, stderr.String(), err
	}
	defer f.Close()
	res, err := imp.Read(f)
	return res, stderr.String(), err
}

// testTemplate is the file the import adds to the program's package. Every identifier it
// declares or imports carries a rotiniImport prefix so it cannot collide with the package's.
const testTemplate = `package %s

import (
	rotiniimport "github.com/go-rotini/import"
	rotiniimportcobra "github.com/go-rotini/import/cobra"
	rotiniimportos "os"
	rotiniimporttesting "testing"
)

func %s(rotiniImportT *rotiniimporttesting.T) {
	rotiniImportResult, rotiniImportErr := rotiniimportcobra.FromCobra((%s).Root(), rotiniimportcobra.Options{Strict: %t})
	if rotiniImportErr != nil {
		rotiniImportT.Fatal(rotiniImportErr)
	}
	if rotiniImportErr := rotiniimport.WriteFile(rotiniimportos.Getenv("ROTINI_IMPORT_OUT"), rotiniImportResult); rotiniImportErr != nil {
		rotiniImportT.Fatal(rotiniImportErr)
	}
}
`

func goEnv() []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GOWORK=") && !strings.HasPrefix(kv, "GOFLAGS=") {
			env = append(env, kv)
		}
	}
	return append(env, "GOWORK=off", "GOFLAGS=")
}

func goCmd(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(goEnv(), env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, stderr.Bytes())
	}
	return string(out)
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func randomSuffix(t *testing.T) string {
	t.Helper()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// snapshot fingerprints every file under dir, so a change anywhere shows.
func snapshot(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s %x\n", p, data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// normalize removes what depends on the machine: the fixture's absolute path and the
// importer's version.
func normalize(r *imp.Result, dir string) {
	r.Source.Importer = ""
	for i := range r.Hooks {
		r.Hooks[i].File = strings.Replace(filepath.ToSlash(r.Hooks[i].File), filepath.ToSlash(dir), "$FIXTURE", 1)
	}
}

// notesText renders the notes as the companion prints them on stderr, then the summary.
func notesText(r *imp.Result) string {
	var b strings.Builder
	for _, n := range r.Notes {
		b.WriteString(n.String())
		b.WriteByte('\n')
	}
	b.WriteString(r.Stats().String())
	b.WriteByte('\n')
	return b.String()
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from the golden file (run with -update to accept):\n%s", name, diff(string(want), string(got)))
	}
}

func diff(want, got string) string {
	wl, gl := strings.Split(want, "\n"), strings.Split(got, "\n")
	var b strings.Builder
	for i := 0; i < len(wl) || i < len(gl); i++ {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		if w != g {
			fmt.Fprintf(&b, "line %d:\n  want %q\n  got  %q\n", i+1, w, g)
			if b.Len() > 4000 {
				break
			}
		}
	}
	return b.String()
}
