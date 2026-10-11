# go-rotini/import

Importers that read an existing Go command-line program's live command tree into a framework-neutral description, which the [rotini](https://github.com/go-rotini/rotini) companion CLI turns into a spec, a conf and handler stubs. Start with `rotini import cobra <pkg>`, `rotini import kong <pkg>` or `rotini import urfave <pkg>`; this repository is what that command runs inside your package.

One module holds the description and one package per framework:

| Package | What it is |
|---|---|
| `github.com/go-rotini/import` | The description: the command tree in the shape of a rotini spec, the notes on what could not be carried across, and pointers to the hooks to port. |
| `github.com/go-rotini/import/cobra` | `FromCobra(*cobra.Command, Options)`, built against Cobra 1.6 and later. |
| `github.com/go-rotini/import/kong` | `FromKong(grammar any, Options)`, built against Kong 0.9 and later. |
| `github.com/go-rotini/import/urfave` | `FromURFave(*cli.Command, Options)`, built against urfave/cli 3.1 and later. |

## Features

- Reads the tree a program has built, so it sees exactly what the program declares: commands, aliases, groups, hidden and deprecated items, persistent and local flags, every value type with a rotini equivalent, defaults, environment variables, required flags, file and directory hints, flag groups and arguments
- Adds the help, version and completion commands and flags a framework otherwise adds only while running, and marks them as builtin
- Names arguments from the usage line (Cobra's `Use`, urfave/cli's `ArgsUsage`) where the framework declares none, sanitised to valid names and de-duplicated
- Reports what it cannot carry across exactly, one note per item, graded `lossy`, `unsupported` or `info`
- Points at every run hook with its file and line, mapped to the matching rotini handler hook
- A versioned JSON description (`"format": "rotini-import/1"`); `Read` refuses a major version it does not know

## Installation

You do not normally install this yourself: `rotini import` adds it to a temporary copy of your `go.mod` for the length of the import and leaves your own files untouched. To call it directly:

```bash
go get github.com/go-rotini/import
```

Requires Go 1.22 or later, and Cobra 1.6, Kong 0.9 or urfave/cli 3.1 or later.

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

### Cobra

| Cobra | rotini | |
|---|---|---|
| `Name()`, `Short`, `Long`, `Aliases`, `Hidden`, `Deprecated`, `Example` | `name`, `summary`, `description`, `aliases`, `hidden`, `deprecated`, `examples` | examples are split into lines |
| `GroupID` and the parent's group title | `group` | |
| the root's display-name annotation | `display_name` | |
| `PersistentFlags()` / local flags | flags with / without `cascading` | rotini also accepts a command's own flags after a sub-command name |
| pflag types, `DefValue`, `NoOptDefVal` | `type`, `default`, `implicit_value` | `time` layouts, custom `pflag.Value` and callback flags are lossy |
| required, filename and dirname annotations | `required`, `complete: {kind: file \| directory}` | a required flag's default is dropped |
| the three flag-group annotations | `required_together`, `at_least_one`, `mutually_exclusive`; the last two over one set become `one_of` | groups mixing a command's own and inherited flags are dropped |
| stock `Args` validators | argument counts | custom validators are probed; their value checks are lost |
| `ValidArgs` | an enum on the first argument, with descriptions | |
| `ValidArgsFunction`, flag completion functions | `complete: {kind: none}` for `NoFileCompletions` | other functions are noted for reimplementing |
| `DisableFlagParsing` | a passthrough command | |
| `Flags().SetInterspersed(false)` | `options_first: true` | read from pflag's unexported setting, which has no getter; dropped with a note on a command that takes no arguments |
| run hooks | hook pointers (file and line) | the code itself is ported by hand |

Flags or commands that a program registers lazily (inside a run hook, an `OnInitialize` function, a plugin loader or a wrapper around `Execute`) are not in the tree when it is walked, so they are not imported. Neither are viper bindings: when viper is linked in, the import says so.

### Kong

| Kong | rotini | |
|---|---|---|
| command `name`, `help`, `Help()` detail, `aliases`, `hidden`, `group` | `name`, `summary`, `description`, `aliases`, `hidden`, `group` | the first line of `help` is the summary |
| flags of a command with sub-commands | flags with `cascading` | |
| `short`, the flag name, `aliases` | `identifiers` | |
| Go field types, `time.Duration`, `time.Time`, `*url.URL`, IP types, slices and maps | `type` | a custom `Mapper`, `MapperValue` or unmarshaler is lossy |
| `default`, `enum`, `required`, `placeholder`, `env` | `default`, `enum`, `required`, `placeholder`, `variable` | a required flag's default is dropped |
| `type:"counter"`, `existingfile`, `existingdir`, `path` | `count`, `existingfile`, `existingdir`, `complete: {kind: file}` | `path`'s `~` expansion is lossy |
| `negatable` | `negatable` | |
| `sep`, `mapsep` | `separator` | |
| `xor`, `and` | `mutually_exclusive` (`one_of` when required), `required_together` | groups spanning commands are dropped |
| positional arguments; a slice takes the rest | arguments | |
| a `passthrough` command or argument | a passthrough command or argument | |
| `kong.VersionFlag` | the builtin version flag | |
| `${name}` variables | resolved from `Options.Vars` | a missing variable is reported and replaced |
| `Run`, `BeforeReset`, `BeforeResolve`, `BeforeApply`, `AfterApply`, `AfterRun` | hook pointers (file and line) | |
| `default:"1"` commands, branching arguments, configuration loaders | notes | rotini has no default command |

### urfave/cli

| urfave/cli | rotini | |
|---|---|---|
| `Name`, `Usage`, `Description`, `Aliases`, `Category`, `Hidden`, `Deprecated` | `name`, `summary`, `description`, `aliases`, `group`, `hidden`, `deprecated` | |
| flags, `Local` | flags with `cascading` unless local | |
| flag names, `Aliases` | `identifiers` | one-letter names are short identifiers |
| the flag's Go type, `Config.Count` | `type`, `count` | `GenericFlag`, `TextFlag` and number bases are lossy |
| `Value`, `Required`, `Category`, `Usage` | `default`, `required`, `group`, `summary` | a required flag's default is dropped |
| `Sources` environment variables | `variable` | file and configuration sources are lossy |
| `TakesFile`, `OnlyOnce`, `BoolWithInverseFlag` | `complete: {kind: file}`, `repeatable: false`, `negatable` | |
| `MutuallyExclusiveFlags` | `mutually_exclusive`, `one_of` when `Required` | an option of several flags is dropped |
| `Arguments` with `Min` and `Max`, else `ArgsUsage` | arguments | with neither, a trailing variadic `args` unless `Options.Strict` |
| `SkipFlagParsing`, `StopOnNthArg` | a passthrough command, `options_first` or a passthrough argument | |
| `Before`, `Action`, `After` | hook pointers (file and line) | flag `Action`s and `Validator`s are noted |
| `DefaultCommand`, `PrefixMatchCommands`, `Suggest` | notes | rotini matches commands exactly and runs none by default |
| `HideHelp`, `HideHelpCommand`, `HideVersion`, `EnableShellCompletion` | builtin flags and commands left out or added | |

## Versioning

The module is released as a whole, with tags `vX.Y.Z`. The description's format has its own major version, and the rotini companion CLI pins the importer version it was tested with.
