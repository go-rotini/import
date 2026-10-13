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

// pePclntab finds the line table of a Windows executable through the runtime's symbols that
// bound it.
func pePclntab(f *pe.File) (data []byte, text uint64) {
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
		return nil, 0
	}
	sect := f.Sections[start.SectionNumber-1]
	all, err := sect.Data()
	if err != nil || int(end.Value) > len(all) {
		return nil, 0
	}
	if t := f.Section(".text"); t != nil {
		text = uint64(t.VirtualAddress)
	}
	return all[start.Value:end.Value], text
}
