// Package corpustest runs an importer over a corpus fixture the way the rotini companion CLI
// does, and compares the result with golden files. Only tests import it.
package corpustest

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

// Update rewrites the golden files instead of comparing with them.
var Update = flag.Bool("update", false, "rewrite the golden files")

// TestFile returns the source of the test file the import adds to the program's package,
// given the package's name and the test's name. Every identifier it declares or imports
// carries a rotiniImport prefix so it cannot collide with the package's.
type TestFile func(pkgName, testName string) string

// HasGo skips t when there is no go command.
func HasGo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go command")
	}
}

// Fixture returns the absolute path of testdata/corpus/<name>.
func Fixture(t *testing.T, name string) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("testdata", "corpus", name))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// Run imports the package pkg of the module at dir through the test file src writes: a
// temporary modfile outside the program's module pointing the importer at this checkout,
// the test file added through -overlay with the package's own test files blanked, and the
// description read back from a file. It returns go's combined output too.
func Run(t *testing.T, dir, pkg string, src TestFile) (*imp.Result, string, error) {
	t.Helper()
	work := t.TempDir()
	shim, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	modfile := filepath.Join(work, "import.mod")
	copyFile(t, filepath.Join(dir, "go.mod"), modfile)
	copyFile(t, filepath.Join(dir, "go.sum"), filepath.Join(work, "import.sum"))

	// The companion runs `go get -modfile=<modfile> github.com/go-rotini/import@<v>`.
	// The corpus points the modfile at this checkout instead.
	goCmd(t, dir, "mod", "edit", "-modfile="+modfile,
		"-require=github.com/go-rotini/import@v0.0.0",
		"-replace=github.com/go-rotini/import="+filepath.Dir(shim))

	listed := goCmd(t, dir, "list", "-modfile="+modfile, "-mod=mod", "-f", "{{.Name}}\n{{.Dir}}", pkg)
	pkgName, pkgDir, ok := strings.Cut(strings.TrimSpace(listed), "\n")
	if !ok {
		t.Fatalf("go list %s: unexpected output %q", pkg, listed)
	}
	pkgDir = strings.TrimSpace(pkgDir)
	testName := "TestRotiniImport_" + randomSuffix(t)
	testFile := filepath.Join(work, "zz_rotini_import_test.go")
	if err := os.WriteFile(testFile, []byte(src(pkgName, testName)), 0o600); err != nil {
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

func goEnv() []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GOWORK=") && !strings.HasPrefix(kv, "GOFLAGS=") {
			env = append(env, kv)
		}
	}
	return append(env, "GOWORK=off", "GOFLAGS=")
}

func goCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = goEnv()
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

// Snapshot fingerprints every file under dir, so a change anywhere shows.
func Snapshot(t *testing.T, dir string) string {
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

// Normalize removes what depends on the machine: the fixture's absolute path and the
// importer's version.
func Normalize(r *imp.Result, dir string) {
	r.Source.Importer = ""
	for i := range r.Hooks {
		r.Hooks[i].File = strings.Replace(filepath.ToSlash(r.Hooks[i].File), filepath.ToSlash(dir), "$FIXTURE", 1)
	}
}

// NotesText renders the notes as the companion prints them on stderr, then the summary.
func NotesText(r *imp.Result) string {
	var b strings.Builder
	for _, n := range r.Notes {
		b.WriteString(n.String())
		b.WriteByte('\n')
	}
	b.WriteString(r.Stats().String())
	b.WriteByte('\n')
	return b.String()
}

// Check imports a fixture and compares its description and notes with the golden files
// <golden>.json and <golden>.notes.txt. The import must leave the fixture's files as they
// were and name the importer's version.
func Check(t *testing.T, golden, fixture, pkg string, src TestFile) {
	t.Helper()
	dir := Fixture(t, fixture)
	before := Snapshot(t, dir)
	res, stderr, err := Run(t, dir, pkg, src)
	if err != nil {
		t.Fatalf("import failed: %v\n%s", err, stderr)
	}
	if after := Snapshot(t, dir); after != before {
		t.Fatalf("the import changed the fixture's files")
	}
	if res.Source.Importer == "" {
		t.Fatal("the description does not name the importer's version")
	}
	Normalize(res, dir)
	var js bytes.Buffer
	if err := imp.Write(&js, res); err != nil {
		t.Fatal(err)
	}
	Golden(t, golden+".json", js.Bytes())
	Golden(t, golden+".notes.txt", []byte(NotesText(res)))
}

// Golden compares got with testdata/golden/<name>, or rewrites it with -update.
func Golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *Update {
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

// Download fetches module@version and returns a writable copy of it. The network corpus
// imports a program from there, as its own main module: go refuses an overlay inside the
// module cache.
func Download(t *testing.T, module string) string {
	t.Helper()
	var info struct {
		Dir   string `json:"Dir"`
		Error string `json:"Error"`
	}
	if err := json.Unmarshal([]byte(goCmd(t, t.TempDir(), "mod", "download", "-json", module)), &info); err != nil {
		t.Fatal(err)
	}
	if info.Error != "" || info.Dir == "" {
		t.Fatalf("go mod download %s: %s", module, info.Error)
	}
	dst := t.TempDir()
	err := filepath.WalkDir(info.Dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(info.Dir, p)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

// Network skips t unless ROTINI_IMPORT_NETWORK=1: the network corpus downloads the programs
// it imports.
func Network(t *testing.T) {
	t.Helper()
	if os.Getenv("ROTINI_IMPORT_NETWORK") != "1" {
		t.Skip("set ROTINI_IMPORT_NETWORK=1 to import the pinned real programs")
	}
	HasGo(t)
}

// Baseline fails t when a count in got is more than 10% away from want's. With -update it
// first rewrites testdata/golden/network-<name>.notes.txt, the notes the baseline was read
// from.
func Baseline(t *testing.T, name string, res *imp.Result, want imp.Stats) {
	t.Helper()
	if *Update {
		Golden(t, "network-"+name+".notes.txt", []byte(NotesText(res)))
	}
	got := res.Stats()
	for _, m := range []struct {
		what      string
		got, want int
	}{
		{"commands", got.Commands, want.Commands},
		{"flags", got.Flags, want.Flags},
		{"lossy notes", got.Lossy, want.Lossy},
		{"unsupported notes", got.Unsupported, want.Unsupported},
		{"info notes", got.Info, want.Info},
	} {
		if tol := (m.want + 9) / 10; m.got < m.want-tol || m.got > m.want+tol {
			t.Errorf("%s: %d, baseline %d (±%d)", m.what, m.got, m.want, tol)
		}
	}
}
