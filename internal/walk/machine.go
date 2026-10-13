package walk

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	imp "github.com/go-rotini/import"
)

// Machine recognizes values a program worked out on the machine the import runs on: paths
// under the home, cache, configuration, temporary or working directory, the host name, and
// the test binary's name read from os.Args[0].
type Machine struct {
	dirs     []machineDir // longest first, so the most specific one is named
	hosts    []string
	testName string // the test binary's name, "" outside go test
	testPath string // the test binary's path as os.Args[0] holds it, "" outside go test
	name     string // the program's name, which replaces testName and testPath
	testRoot string // the root's name when it was the test binary's name or path, else ""
	// sharedTemp is the system temporary directory when it is a shared one, such as /tmp,
	// which find does not look for, else "".
	sharedTemp string
}

// machineDir is a directory of this machine: what a note calls it, and the token a note
// writes in its place.
type machineDir struct{ dir, what, token string }

// NewMachine reads this machine's directories and host name. program is the imported
// program's name.
func NewMachine(program string) *Machine {
	m := &Machine{name: program}
	add := func(dir, what, token string) {
		dir = strings.TrimRight(filepath.ToSlash(dir), "/")
		if len(dir) > 1 && !sharedDirs[dir] {
			m.dirs = append(m.dirs, machineDir{dir, what, token})
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		add(home, "the home directory", "~")
	}
	if dir, err := os.UserCacheDir(); err == nil {
		add(dir, "the user cache directory", "<cache>")
	}
	if dir, err := os.UserConfigDir(); err == nil {
		add(dir, "the user configuration directory", "<config>")
	}
	temp := os.TempDir()
	add(temp, "the temporary directory", "<temp>")
	if t := strings.TrimRight(filepath.ToSlash(temp), "/"); sharedDirs[t] {
		m.sharedTemp = t
	}
	if dir, err := os.Getwd(); err == nil {
		add(dir, "the working directory", "<cwd>")
	}
	sort.SliceStable(m.dirs, func(i, j int) bool { return len(m.dirs[i].dir) > len(m.dirs[j].dir) })
	if host, err := os.Hostname(); err == nil {
		m.hosts = hostNames(host)
	}
	if base := testBinary(); base != "" && base != program {
		m.testName = base
		if os.Args[0] != base {
			m.testPath = os.Args[0]
		}
	}
	return m
}

// RenamedRoot records that the root's name, orig, was the test binary's name or path and was
// imported as the program's name, so [Machine.Scrub] reports both renamings in one note.
func (m *Machine) RenamedRoot(orig string) { m.testRoot = orig }

// testBinary is the running test binary's name, without ".exe", or "" outside go test.
func testBinary() string {
	base := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if !strings.HasSuffix(base, ".test") {
		return ""
	}
	return base
}

// ProgramName returns the program's name, the test binary's base name without ".exe" and the
// ".test" suffix go test adds, when name is what a program that names itself from os.Args[0]
// reads under the import: the test binary's name or its full path. It reports whether it did.
// A name is never made from the directories in the path.
func ProgramName(name string) (string, bool) {
	base := testBinary()
	if base == "" || !isTestBinary(name, base) {
		return name, false
	}
	return strings.TrimSuffix(base, ".test"), true
}

// isTestBinary reports whether name is os.Args[0] or names the test binary base. A path
// that holds a space is cut there by a framework that takes a command's name as the first
// word of its usage line, so the part before the first space counts too.
func isTestBinary(name, base string) bool {
	arg0 := os.Args[0]
	if name == "" {
		return false
	}
	if name == arg0 || strings.HasPrefix(arg0, name+" ") {
		return true
	}
	return strings.TrimSuffix(filepath.Base(name), ".exe") == base
}

// CutProgram replaces the test binary's full path at the start of a usage line with its base
// name, so the words of a path that holds spaces are not read as arguments.
func CutProgram(use string) string {
	arg0 := os.Args[0]
	if testBinary() == "" || arg0 == filepath.Base(arg0) {
		return use
	}
	if rest, ok := strings.CutPrefix(use, arg0); ok && (rest == "" || rest[0] == ' ') {
		return filepath.Base(arg0) + rest
	}
	return use
}

// sharedDirs are system-wide temporary directories. A program that writes one literally
// cannot be told apart from one that asked the system for it, so they are not looked for.
var sharedDirs = map[string]bool{
	"/tmp": true, "/var/tmp": true, "/usr/tmp": true, "/private/tmp": true, "/private/var/tmp": true,
}

// hostNames lists the names of the host called host that are looked for: the full name and
// its first label, each only when it holds a letter and also a digit, a dot or a hyphen. A
// plain word such as "ubuntu" or "server", or a number such as "2024", is as likely to be a
// value the program declares, and "localhost" names every machine.
func hostNames(host string) []string {
	short, _, _ := strings.Cut(host, ".")
	if strings.EqualFold(short, "localhost") {
		return nil
	}
	var out []string
	for _, h := range []string{host, short} {
		if len(h) >= 4 && strings.ContainsAny(h, "0123456789.-") && strings.IndexFunc(h, IsLetter) >= 0 && (len(out) == 0 || out[0] != h) {
			out = append(out, h)
		}
	}
	return out
}

// find names what s holds that was worked out on this machine, or returns "".
func (m *Machine) find(s string) string {
	slashed := filepath.ToSlash(s)
	for _, d := range m.dirs {
		if containsWord(slashed, d.dir, "/") {
			return d.what
		}
	}
	for _, h := range m.hosts {
		if containsWord(s, h, "") {
			return "the host name"
		}
	}
	if m.testName != "" && containsWord(s, m.testName, "") {
		return "the test binary's name"
	}
	return ""
}

// value applies find to every string in a default.
func (m *Machine) value(v any) string { return eachString(v, m.find) }

// eachString applies find to every string in a default, map keys included, and returns the
// first result that is not "".
func eachString(v any, find func(string) string) string {
	switch t := v.(type) {
	case string:
		return find(t)
	case []any:
		for _, e := range t {
			if w := eachString(e, find); w != "" {
				return w
			}
		}
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if w := find(k); w != "" {
				return w
			}
			if w := eachString(t[k], find); w != "" {
				return w
			}
		}
	}
	return ""
}

// underSharedTemp reports whether a default holds a path in the shared system temporary
// directory.
func (m *Machine) underSharedTemp(v any) bool {
	if m.sharedTemp == "" {
		return false
	}
	return eachString(v, func(s string) string {
		if containsWord(filepath.ToSlash(s), m.sharedTemp, "/") {
			return m.sharedTemp
		}
		return ""
	}) != ""
}

// text replaces the test binary's path and name in help text with the program's name.
func (m *Machine) text(s string) (string, bool) {
	if m.testName == "" {
		return s, false
	}
	pathHit := false
	if m.testPath != "" {
		for _, p := range pathForms(m.testPath) {
			var ok bool
			s, ok = replaceWord(s, p, "", m.name)
			pathHit = pathHit || ok
		}
	}
	s, ok := replaceWord(s, m.testName, "", m.name)
	return s, ok || pathHit
}

// pathForms lists the ways text can spell a path: as it is, with backslashes for slashes,
// and with each backslash doubled, as Go's %q quotes one.
func pathForms(p string) []string {
	back := strings.ReplaceAll(p, "/", `\`)
	out := []string{p}
	for _, f := range []string{back, strings.ReplaceAll(back, `\`, `\\`)} {
		if !slices.Contains(out, f) {
			out = append(out, f)
		}
	}
	return out
}

// tokens writes this machine's directories and host name in help text as the tokens
// [Machine.shown] uses, and lists what it found, each as a note names it and its token.
func (m *Machine) tokens(s string) (string, [][2]string) {
	// A token can leave a directory standing as a word that was not one before
	// ("host/home/me"), so the text is scanned until a pass finds nothing. No token holds
	// a directory or host name, so this ends.
	var found [][2]string
	for {
		out, more := m.tokensOnce(s)
		if len(more) == 0 {
			return out, found
		}
		s = out
		for _, f := range more {
			if !slices.Contains(found, f) {
				found = append(found, f)
			}
		}
	}
}

func (m *Machine) tokensOnce(s string) (string, [][2]string) {
	var found [][2]string
	for _, d := range m.dirs {
		hit := false
		for _, dir := range pathForms(d.dir) {
			var ok bool
			s, ok = replaceWord(s, dir, "/", d.token)
			hit = hit || ok
		}
		if hit {
			found = append(found, [2]string{d.what, d.token})
		}
	}
	for _, h := range m.hosts {
		var ok bool
		if s, ok = replaceWord(s, h, "", "<host>"); ok {
			found = append(found, [2]string{"the host name", "<host>"})
			break
		}
	}
	return s, found
}

// replaceWord replaces each occurrence of word in s that is not part of a longer name, as
// [containsWord] finds them, with repl, and reports whether there was one.
func replaceWord(s, word, extra, repl string) (string, bool) {
	if wordIndex(s, word, extra) < 0 {
		return s, false
	}
	var b strings.Builder
	for {
		i := wordIndex(s, word, extra)
		if i < 0 {
			b.WriteString(s)
			return b.String(), true
		}
		b.WriteString(s[:i])
		b.WriteString(repl)
		s = s[i+len(word):]
	}
}

// shown is a default as a note quotes it, with this machine's directories and host name
// written as tokens: "~/.config/app", "<temp>/x", "<host>".
func (m *Machine) shown(v any) string {
	s := filepath.ToSlash(fmt.Sprint(v))
	for _, d := range m.dirs {
		s = strings.ReplaceAll(s, d.dir, d.token)
	}
	for _, h := range m.hosts {
		s = strings.ReplaceAll(s, h, "<host>")
	}
	return s
}

// ScrubNotes writes the program's name where a note holds the test binary's name, and this
// machine's directories and host name as tokens, as [Machine.Scrub] does for help text. Call
// it on the notes written before Scrub runs, since some quote the values Scrub is for.
func (m *Machine) ScrubNotes(notes []imp.Note) {
	for i := range notes {
		notes[i].Msg = m.ScrubNote(notes[i].Msg)
	}
}

// ScrubNote is [Machine.ScrubNotes] for one note's text.
func (m *Machine) ScrubNote(msg string) string {
	msg, _ = m.text(msg)
	msg, _ = m.tokens(msg)
	return msg
}

// Scrub walks the imported tree under c, whose path is path. It writes the program's name
// where help text holds the test binary's name, writes this machine's directories and host
// name in help text as tokens ("~/.app.yaml"), and drops each default or implicit value
// worked out on this machine. It reports the tokens once per command, each dropped value,
// and the renaming once, together with the root's when [Machine.RenamedRoot] recorded it,
// through note.
func (m *Machine) Scrub(c *imp.Command, path string, note func(path string, level imp.Level, msg string)) {
	n := m.scrub(c, path, note)
	commands := "one command"
	if n > 1 {
		commands = fmt.Sprintf("%d commands", n)
	}
	switch {
	case m.testRoot != "":
		msg := fmt.Sprintf("the root's name %q is the test binary's name, which the program read from os.Args[0]; it was imported as %q", m.testRoot, m.name)
		if strings.ContainsAny(m.testRoot, `/\`) {
			msg = fmt.Sprintf("the root's name is the test binary's path, which the program read from os.Args[0]; it was imported as %q", m.name)
		}
		if n > 0 {
			msg += ", and the test binary's name was replaced by it in the help text of " + commands
		}
		note(path, imp.Info, msg)
	case n > 0:
		note(path, imp.Info, fmt.Sprintf("the test binary's name %q, which the program read from os.Args[0], was replaced by %q in %s", m.testName, m.name, commands))
	}
}

// scrub is Scrub for one command and those under it. It returns how many of them had the
// test binary's name replaced.
func (m *Machine) scrub(c *imp.Command, path string, note func(path string, level imp.Level, msg string)) int {
	cs := &commandScrub{m: m, path: path, note: note}
	cs.text(&c.Summary)
	cs.text(&c.Description)
	cs.text(&c.Deprecated)
	for i := range c.Examples {
		cs.text(&c.Examples[i])
	}
	for _, f := range c.Flags {
		cs.text(&f.Summary)
		cs.text(&f.Deprecated)
		cs.schema("--"+f.Name, f.Schema)
	}
	for _, a := range c.Arguments {
		cs.text(&a.Summary)
		cs.schema("<"+a.Name+">", a.Schema)
	}
	if len(cs.held) > 0 {
		whats, tokens := make([]string, len(cs.held)), make([]string, len(cs.held))
		for i, h := range cs.held {
			whats[i], tokens[i] = h[0], strconv.Quote(h[1])
		}
		note(path, imp.Lossy, fmt.Sprintf("help text held %s of the machine the import ran on; it was written as %s", andList(whats), andList(tokens)))
	}
	count := 0
	if cs.renamed {
		count++
	}
	for _, sub := range c.Commands {
		count += m.scrub(sub, JoinPath(path, sub.Name), note)
	}
	return count
}

// commandScrub is the scrubbing of one command.
type commandScrub struct {
	m       *Machine
	path    string
	note    func(path string, level imp.Level, msg string)
	renamed bool        // the test binary's name was replaced
	held    [][2]string // what help text held, and the token written for it
}

// text scrubs one piece of help text.
func (cs *commandScrub) text(s *string) {
	if out, ok := cs.m.text(*s); ok {
		*s, cs.renamed = out, true
	}
	out, found := cs.m.tokens(*s)
	*s = out
	for _, f := range found {
		if !slices.Contains(cs.held, f) {
			cs.held = append(cs.held, f)
		}
	}
}

// schema scrubs the enum summaries, implicit values and defaults of a value and its items.
func (cs *commandScrub) schema(what string, s *imp.Schema) {
	m := cs.m
	for ; s != nil; s = s.Items {
		for i := range s.Enum {
			cs.text(&s.Enum[i].Summary)
		}
		if s.ImplicitValue != "" {
			if out, ok := m.text(s.ImplicitValue); ok && m.find(out) == "" {
				s.ImplicitValue, cs.renamed = out, true
			} else if w := m.find(s.ImplicitValue); w != "" {
				cs.note(cs.path, imp.Lossy, fmt.Sprintf("%s: the value %q it takes when given alone holds %s of the machine the import ran on, so it was dropped and the flag needs a value; set it in the handler", what, m.shown(s.ImplicitValue), w))
				s.ImplicitValue = ""
			}
		}
		if s.Default == nil {
			continue
		}
		if str, ok := s.Default.(string); ok {
			if out, ok := m.text(str); ok && m.find(out) == "" {
				s.Default, cs.renamed = out, true
				continue
			}
		}
		if w := m.value(s.Default); w != "" {
			cs.note(cs.path, imp.Lossy, fmt.Sprintf("%s: the default %q holds %s of the machine the import ran on, so it was dropped; declare it with expand: or set it in the handler", what, m.shown(s.Default), w))
			s.Default = nil
		} else if m.underSharedTemp(s.Default) {
			cs.note(cs.path, imp.Info, fmt.Sprintf("%s: the default %q is under the system temporary directory, which other systems put elsewhere; if the program asks the system for it, declare it with expand:", what, m.shown(s.Default)))
		}
	}
}

// andList joins words as prose: "a", "a and b", "a, b and c".
func andList(words []string) string {
	if len(words) < 2 {
		return strings.Join(words, "")
	}
	return strings.Join(words[:len(words)-1], ", ") + " and " + words[len(words)-1]
}

// containsWord reports whether word occurs in s, not as part of a longer name. extra lists
// further characters that may follow it, such as "/" after a directory.
func containsWord(s, word, extra string) bool { return wordIndex(s, word, extra) >= 0 }

func wordIndex(s, word, extra string) int {
	for from := 0; from <= len(s)-len(word); {
		i := strings.Index(s[from:], word)
		if i < 0 {
			return -1
		}
		i += from
		end := i + len(word)
		before := i == 0 || !nameByte(s[i-1])
		after := end == len(s) || !nameByte(s[end]) || strings.IndexByte(extra, s[end]) >= 0
		if before && after {
			return i
		}
		from = i + 1
	}
	return -1
}

func nameByte(b byte) bool {
	return IsLetter(rune(b)) || IsDigit(rune(b)) || b == '_' || b == '-' || b == '.'
}

// Names hands out names unique among one command's flags or sub-commands.
type Names map[string]bool

// Unique returns name, or name with the first free numeric suffix ("name-2") when name is
// taken, and marks the result taken.
func (n Names) Unique(name string) string {
	out := name
	for i := 2; n[out]; i++ {
		out = fmt.Sprintf("%s-%d", name, i)
	}
	n[out] = true
	return out
}

// Rename makes orig a valid rotini name unique among n, as [CommandName] and
// [Names.Unique] do. why says why the name changed, for a note, or is "" when it did not.
func (n Names) Rename(orig string) (name, why string) {
	valid := CommandName(orig)
	name = n.Unique(valid)
	switch {
	case name != valid:
		why = fmt.Sprintf("is %q once made a valid rotini name, which another already has", valid)
	case name != orig:
		why = "is not a valid rotini name"
	}
	return name, why
}

// RenameFlag is [Names.Rename] for a flag of a command that inherits the cascading flags in
// inherited, which maps their imported names to their names in the framework. A flag that
// had to be renamed, and whose new name is an inherited flag's imported name, gets a numeric
// suffix instead, so it does not shadow the flag it inherits. A flag whose own name is valid
// keeps it, overriding an inherited flag of that name as the framework does; when that
// inherited flag was renamed to it, shadowed is the inherited flag's name in the framework,
// which can no longer be given under this command.
func (n Names) RenameFlag(orig string, inherited map[string]string) (name, why, shadowed string) {
	valid := CommandName(orig)
	from, ok := inherited[valid]
	if !ok || from == orig {
		name, why = n.Rename(orig)
		return name, why, ""
	}
	if valid == orig {
		name, why = n.Rename(orig)
		if name == orig {
			shadowed = from
		}
		return name, why, shadowed
	}
	for i := 2; ; i++ {
		name = fmt.Sprintf("%s-%d", valid, i)
		if _, taken := inherited[name]; !taken && !n[name] {
			break
		}
	}
	n[name] = true
	if from == valid {
		return name, fmt.Sprintf("would be --%s, the name of an inherited flag", valid), ""
	}
	return name, fmt.Sprintf("would be --%s, the name the inherited flag --%s was imported as", valid, from), ""
}

// ShadowNote is the note for a flag that keeps its name and so overrides an inherited flag
// renamed to it, as [Names.RenameFlag] reports one. Its verbs are the flag's name, the
// inherited flag's name in the framework, and the flag's name again.
const ShadowNote = "flag --%s overrides the inherited --%s, which was imported as --%s, so the inherited flag can't be given under this command"

// Cascade returns the cascading flags a command's sub-commands inherit, as
// [Names.RenameFlag] takes them: those the command inherits, and those of its flags that
// cascade. orig holds a flag's name in the framework; a flag missing from it is taken to
// have kept its name.
func Cascade(inherited map[string]string, flags []*imp.Flag, orig map[*imp.Flag]string) map[string]string {
	out := make(map[string]string, len(inherited)+len(flags))
	for k, v := range inherited {
		out[k] = v
	}
	for _, f := range flags {
		if !f.Cascading {
			continue
		}
		if o, ok := orig[f]; ok {
			out[f.Name] = o
		} else {
			out[f.Name] = f.Name
		}
	}
	return out
}

// SharedHooks returns a message for each closure that is the same hook of several commands:
// a wrapper, such as one that records telemetry before calling the command's own function,
// which every hook pointer then names. Closures are matched by where they are written, since
// one the compiler inlined into each caller can carry a different name in each.
func SharedHooks(hooks []imp.Hook) []string {
	type key struct {
		framework, file string
		line            int
		fn              string // only when the file is unknown
	}
	type group struct {
		fn    string // the closure's name, "" when the commands' names for it differ
		count int
	}
	groups := map[key]*group{}
	var order []key
	for _, h := range hooks {
		if !strings.Contains(h.Func, ".func") {
			continue
		}
		k := key{framework: h.Framework, file: h.File, line: h.Line}
		if h.File == "" {
			k.fn = h.Func
		}
		g := groups[k]
		if g == nil {
			g = &group{fn: h.Func}
			groups[k] = g
			order = append(order, k)
		} else if g.fn != h.Func {
			g.fn = ""
		}
		g.count++
	}
	var out []string
	for _, k := range order {
		g := groups[k]
		if g.count < 2 {
			continue
		}
		what := g.fn
		if what == "" {
			what = fmt.Sprintf("the closure at %s:%d", k.file, k.line)
		}
		out = append(out, fmt.Sprintf("%s is the %s of %d commands: it is a closure one function made for each, so the code to port is in the function each command passed to it", what, k.framework, g.count))
	}
	return out
}
