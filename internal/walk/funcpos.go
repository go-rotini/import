package walk

import (
	"debug/elf"
	"debug/gosym"
	"debug/macho"
	"debug/pe"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// maxWrapperSize bounds the scan of a method value's wrapper for the method inlined into it.
const maxWrapperSize = 4096

// modCache is the module cache directory with forward slashes, as the runtime writes file
// names, or "" when it cannot be worked out.
func modCache() string {
	dir := os.Getenv("GOMODCACHE")
	if dir == "" {
		gopath := os.Getenv("GOPATH")
		if gopath == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return ""
			}
			gopath = filepath.Join(home, "go")
		}
		dir = filepath.Join(filepath.SplitList(gopath)[0], "pkg", "mod")
	}
	return strings.TrimSuffix(filepath.ToSlash(dir), "/")
}

// methodPos locates the method a method value's wrapper fn calls: inlined into the wrapper,
// or as a function of its own in the running binary's symbol table.
func methodPos(fn *runtime.Func, method string) (file string, line int) {
	entry := fn.Entry()
	for pc := entry; pc < entry+maxWrapperSize; pc++ {
		if f := runtime.FuncForPC(pc); f == nil || f.Entry() != entry {
			break
		}
		frame, _ := runtime.CallersFrames([]uintptr{pc + 1}).Next()
		if frame.Function == method {
			return frame.File, frame.Line
		}
	}
	tab := symbols()
	if tab == nil {
		return "", 0
	}
	f := tab.LookupFunc(method)
	if f == nil {
		return "", 0
	}
	file, line, _ = tab.PCToLine(f.Entry)
	return file, line
}

// symbols reads the running binary's line table, or nil when it cannot.
var symbols = sync.OnceValue(func() *gosym.Table {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	data, text := pclntab(exe)
	if data == nil {
		return nil
	}
	tab, err := gosym.NewTable(nil, gosym.NewLineTable(data, text))
	if err != nil {
		return nil
	}
	return tab
})

// pclntab returns the Go line table of the executable at path and the address of its text.
func pclntab(path string) (data []byte, text uint64) {
	if f, err := elf.Open(path); err == nil {
		defer f.Close()
		if s, t := f.Section(".gopclntab"), f.Section(".text"); s != nil && t != nil {
			if data, err := s.Data(); err == nil {
				return data, t.Addr
			}
		}
		return nil, 0
	}
	if f, err := macho.Open(path); err == nil {
		defer f.Close()
		if s, t := f.Section("__gopclntab"), f.Section("__text"); s != nil && t != nil {
			if data, err := s.Data(); err == nil {
				return data, t.Addr
			}
		}
		return nil, 0
	}
	if f, err := pe.Open(path); err == nil {
		defer f.Close()
		return pePclntab(f)
	}
	return nil, 0
}

// pePclntab finds the line table of a Windows executable: through the runtime's symbols that
// bound it, or, in an executable built without symbols (as go test builds one, or a build
// with -ldflags=-s), by its header.
func pePclntab(f *pe.File) (data []byte, text uint64) {
	if t := f.Section(".text"); t != nil {
		text = uint64(t.VirtualAddress)
	}
	if data := peSymbolPclntab(f); data != nil {
		return data, text
	}
	return scanPclntab(f, text), text
}

// peSymbolPclntab returns the line table the runtime.pclntab and runtime.epclntab symbols
// bound, or nil.
func peSymbolPclntab(f *pe.File) []byte {
	var start, end *pe.Symbol
	for _, s := range f.Symbols {
		switch s.Name {
		case "runtime.pclntab":
			start = s
		case "runtime.epclntab":
			end = s
		}
	}
	if start == nil || end == nil || start.SectionNumber != end.SectionNumber ||
		start.SectionNumber < 1 || int(start.SectionNumber) > len(f.Sections) || end.Value < start.Value {
		return nil
	}
	sect := f.Sections[start.SectionNumber-1]
	all, err := sect.Data()
	if err != nil || int(end.Value) > len(all) {
		return nil
	}
	return all[start.Value:end.Value]
}

// scanPclntab finds the line table in a read-only data section by its header: a magic
// number of a release the line table reader knows, two zero bytes, the instruction size
// quantum and the pointer size. A match counts only once it parses as a table that holds
// runtime.main, so bytes that merely look like a header are passed over.
func scanPclntab(f *pe.File, text uint64) []byte {
	for _, s := range f.Sections {
		if s.Name != ".rdata" {
			continue
		}
		all, err := s.Data()
		if err != nil {
			continue
		}
		for i := 0; i+8 <= len(all); i += 4 {
			h := all[i : i+8]
			if !pclntabMagic[h[0]] || h[1] != 0xff || h[2] != 0xff || h[3] != 0xff || h[4] != 0 || h[5] != 0 ||
				(h[6] != 1 && h[6] != 2 && h[6] != 4) || (h[7] != 4 && h[7] != 8) {
				continue
			}
			if holdsRuntimeMain(all[i:], text) {
				return all[i:]
			}
		}
	}
	return nil
}

// holdsRuntimeMain reports whether data parses as a line table that holds runtime.main. The
// reader assumes a well-formed table, so a panic on bytes that only look like one counts as
// no.
func holdsRuntimeMain(data []byte, text uint64) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	tab, err := gosym.NewTable(nil, gosym.NewLineTable(data, text))
	return err == nil && tab.LookupFunc("runtime.main") != nil
}

// pclntabMagic holds the low bytes of the little-endian line table magic numbers: Go 1.16
// (0xfffffffa), Go 1.18 (0xfffffff0) and Go 1.20 on (0xfffffff1).
var pclntabMagic = map[byte]bool{0xfa: true, 0xf0: true, 0xf1: true}
