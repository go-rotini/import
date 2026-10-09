# go-rotini/import

Importers that read an existing Go command-line program's live command tree into a framework-neutral description, which the [rotini](https://github.com/go-rotini/rotini) companion CLI turns into a spec, a conf and handler stubs. Start with `rotini import cobra <pkg>`; this repository is what that command runs inside your package.

The repository holds one module per framework, so importing one framework never adds another to your module graph:

| Module | Import path | What it is |
|---|---|---|
| root | `github.com/go-rotini/import` | The description: the command tree in the shape of a rotini spec, the notes on what could not be carried across, and pointers to the hooks to port. Standard library only. |
| cobra | `github.com/go-rotini/import/cobra` | `FromCobra(*cobra.Command, Options)`, built against Cobra 1.6 and later. |

## Features

- Reads the tree a program has built, so it sees exactly what the program declares: commands, aliases, groups, hidden and deprecated items, persistent and local flags, every pflag type with a rotini equivalent, defaults, implicit values, required flags, file and directory hints, flag groups, and stock argument validators (counts found by probing)
- Asks Cobra to add the help and completion commands and the help and version flags it otherwise adds only while executing, and marks them as builtin
- Names arguments from the `Use` line, sanitised to valid names and de-duplicated
- Reports what it cannot carry across exactly, one note per item, graded `lossy`, `unsupported` or `info`
- Points at every run hook with its file and line, mapped to the matching rotini handler hook
- A versioned JSON description (`"format": "rotini-import/1"`); `Read` refuses a major version it does not know

## Installation

You do not normally install this yourself: `rotini import cobra` adds it to a temporary copy of your `go.mod` for the length of the import and leaves your own files untouched. To call it directly:

```bash
go get github.com/go-rotini/import/cobra
```

Requires Go 1.22 or later, and Cobra 1.6 or later.

## Quick Start

```go
package main

import (
	"log"
	"os"

	imp "github.com/go-rotini/import"
	cobraimport "github.com/go-rotini/import/cobra"

	"example.com/acme/cmd"
)

func main() {
	res, err := cobraimport.FromCobra(cmd.NewRootCmd(), cobraimport.Options{})
	if err != nil {
		log.Fatal(err)
	}
	for _, n := range res.Notes {
		log.Println(n) // [lossy] acme deploy: --at is a time flag: …
	}
	if err := imp.Write(os.Stdout, res); err != nil {
		log.Fatal(err)
	}
}
```

## What carries across

| Cobra | rotini | |
|---|---|---|
| `Name()`, `Short`, `Long`, `Aliases`, `Hidden`, `Deprecated`, `Example` | `name`, `summary`, `description`, `aliases`, `hidden`, `deprecated`, `examples` | examples are split into lines |
| `GroupID` and the parent's group title | `group` | |
| the root's display-name annotation | `display_name` | |
| `PersistentFlags()` / local flags | flags with / without `cascading` | rotini also accepts a command's own flags after a sub-command name |
| pflag types, `DefValue`, `NoOptDefVal` | `type`, `default`, `implicit_value` | `time` layouts, custom `pflag.Value` and callback flags are lossy |
| required, filename and dirname annotations | `required`, `complete: {kind: file \| directory}` | |
| the three flag-group annotations | `required_together`, `at_least_one`, `mutually_exclusive`; the last two over one set become `one_of` | groups mixing a command's own and inherited flags are dropped |
| stock `Args` validators | argument counts | custom validators are probed; their value checks are lost |
| `ValidArgs` | an enum on the first argument, with descriptions | |
| `ValidArgsFunction`, flag completion functions | `complete: {kind: none}` for `NoFileCompletions` | other functions are noted for reimplementing |
| `DisableFlagParsing` | a passthrough command | |
| run hooks | hook pointers (file and line) | the code itself is ported by hand |

Flags or commands that a program registers lazily (inside a run hook, an `OnInitialize` function, a plugin loader or a wrapper around `Execute`) are not in the tree when it is walked, so they are not imported. Neither are viper bindings: when viper is linked in, the import says so.

## Versioning

Each module is versioned and released on its own: tags `vX.Y.Z` for the root module and `cobra/vX.Y.Z` for the Cobra importer. The description's format has its own major version, and the rotini companion CLI pins the importer version it was tested with.
