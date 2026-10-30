package imp

import "fmt"

// Format names this description's layout and its major version. [Write] always writes it,
// and [Read] refuses a description whose major version differs.
const Format = "rotini-import/1"

// Result is one imported program.
type Result struct {
	// Format is [Format]; Write sets it.
	Format string `json:"format"`
	// Source says what the program was built with and which importer read it.
	Source Source `json:"source"`
	// Command is the root command.
	Command *Command `json:"command"`
	// Notes lists what the import could not carry across exactly, in walk order.
	Notes []Note `json:"notes,omitempty"`
	// Hooks points at the code that runs at each lifecycle step, for porting by hand.
	Hooks []Hook `json:"hooks,omitempty"`
}

// Source identifies the framework and the importer behind a [Result].
type Source struct {
	// Framework is the framework's short name, such as "cobra".
	Framework string `json:"framework"`
	// Version is the framework module's version in the imported program, when known. A
	// module replaced by a local directory has none under Go releases before 1.27.
	Version string `json:"version,omitempty"`
	// Importer is the importer module's version, when known, with the same limit.
	Importer string `json:"importer,omitempty"`
}

// Command is one command, shaped like a command in a rotini spec. Field names follow the
// spec's keys.
type Command struct {
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name,omitempty"`
	Aliases     []string `json:"aliases,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	Description string   `json:"description,omitempty"`
	Examples    []string `json:"examples,omitempty"`
	Group       string   `json:"group,omitempty"`
	Hidden      bool     `json:"hidden,omitempty"`
	Deprecated  string   `json:"deprecated,omitempty"`
	// Passthrough makes every word after the command's name reach its last argument as typed,
	// with no flag parsing.
	Passthrough bool `json:"passthrough,omitempty"`
	// OptionsFirst stops flag parsing at the command's first argument: from that word on,
	// every word is an argument.
	OptionsFirst bool         `json:"options_first,omitempty"`
	Arguments    []*Argument  `json:"arguments,omitempty"`
	Flags        []*Flag      `json:"flags,omitempty"`
	FlagGroups   []*FlagGroup `json:"flag_groups,omitempty"`
	Commands     []*Command   `json:"commands,omitempty"`
	// Builtin marks a command the framework adds on its own: "help" or "completion".
	// It is not a spec key; a spec writer uses it to emit its own seed for that command.
	Builtin string `json:"builtin,omitempty"`
}

// Argument is one positional argument.
type Argument struct {
	Name    string `json:"name"`
	Summary string `json:"summary,omitempty"`
	// Passthrough makes this argument and every word after it reach the argument as typed,
	// while the flags before it parse as usual. It is the command's last argument.
	Passthrough bool    `json:"passthrough,omitempty"`
	Schema      *Schema `json:"schema"`
}

// Flag is one flag.
type Flag struct {
	Name        string   `json:"name"`
	Summary     string   `json:"summary,omitempty"`
	Identifiers []string `json:"identifiers"`
	// Group is the heading the flag is listed under in help.
	Group                 string   `json:"group,omitempty"`
	Cascading             bool     `json:"cascading,omitempty"`
	ShortCircuit          bool     `json:"short_circuit,omitempty"`
	Hidden                bool     `json:"hidden,omitempty"`
	Deprecated            string   `json:"deprecated,omitempty"`
	DeprecatedIdentifiers []string `json:"deprecated_identifiers,omitempty"`
	// HiddenIdentifiers are further spellings the program accepts for the flag that its help
	// does not list, such as "--log_level" for "--log-level". They are not in Identifiers.
	HiddenIdentifiers []string `json:"hidden_identifiers,omitempty"`
	Schema            *Schema  `json:"schema"`
	// Builtin marks a flag the framework adds on its own: "help" or "version".
	// It is not a spec key; a spec writer uses it to emit its own seed for that flag.
	Builtin string `json:"builtin,omitempty"`
}

// Schema is an input's value schema, shaped like an input schema in a rotini spec.
type Schema struct {
	Type          string      `json:"type"`
	Items         *Schema     `json:"items,omitempty"`
	Required      bool        `json:"required,omitempty"`
	Default       any         `json:"default,omitempty"`
	Enum          []EnumValue `json:"enum,omitempty"`
	MinItems      *int        `json:"minItems,omitempty"`
	MaxItems      *int        `json:"maxItems,omitempty"`
	Separator     string      `json:"separator,omitempty"`
	ImplicitValue string      `json:"implicit_value,omitempty"`
	Placeholder   string      `json:"placeholder,omitempty"`
	Complete      *Complete   `json:"complete,omitempty"`
	// Completions are values shell completion offers for the input without restricting it:
	// any other value is accepted too, unlike Enum.
	Completions []EnumValue `json:"completions,omitempty"`
	// Variable lists the environment variables the input falls back to, first preferred.
	Variable []string `json:"variable,omitempty"`
	// Negatable is true when a bool flag also takes a --no-<name> form, or the one negated
	// identifier it takes instead, such as "--plain".
	Negatable any `json:"negatable,omitempty"`
	// Repeatable, when false, makes giving a single-value flag more than once an error.
	Repeatable *bool `json:"repeatable,omitempty"`
}

// EnumValue is one allowed value, with an optional one-line description.
type EnumValue struct {
	Value   string `json:"value"`
	Summary string `json:"summary,omitempty"`
}

// Complete is a shell-completion hint for an input's value.
type Complete struct {
	// Kind is "file", "directory" or "none".
	Kind       string   `json:"kind"`
	Extensions []string `json:"extensions,omitempty"`
}

// FlagGroup is a constraint over flags of one command. Kind is one of
// "mutually_exclusive", "required_together", "one_of" and "at_least_one".
type FlagGroup struct {
	Kind  string   `json:"kind"`
	Flags []string `json:"flags"`
}

// Level grades a [Note].
type Level string

// The note levels.
const (
	// Lossy means carried across, but something about it changed or was lost.
	Lossy Level = "lossy"
	// Unsupported means dropped, because rotini has no equivalent.
	Unsupported Level = "unsupported"
	// Info means carried across faithfully, with something worth knowing.
	Info Level = "info"
)

// Note is one thing the import could not map exactly.
type Note struct {
	// Path is the command path, names joined by spaces, such as "acme deploy".
	Path  string `json:"path"`
	Level Level  `json:"level"`
	Msg   string `json:"msg"`
}

// String renders the note as one line: "[lossy] acme deploy: …".
func (n Note) String() string { return fmt.Sprintf("[%s] %s: %s", n.Level, n.Path, n.Msg) }

// Hook points at code that runs at one lifecycle step of a command and has to be ported
// by hand.
type Hook struct {
	// Path is the command path.
	Path string `json:"path"`
	// Framework is the framework's name for the step, such as "PersistentPreRunE".
	Framework string `json:"framework"`
	// Rotini is the matching rotini handler hook, such as "CascadingPreRun".
	Rotini string `json:"rotini"`
	// Func is the function's full name, as the Go runtime reports it.
	Func string `json:"func,omitempty"`
	// File and Line locate the function. File is absolute for code in the program's own
	// module and module-cache relative ("example.com/x@v1.2.3/cmd.go") for a dependency.
	// Both are empty when a method value's method cannot be located.
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
}

// Stats counts what a [Result] holds.
type Stats struct {
	Commands    int
	Flags       int
	Lossy       int
	Unsupported int
	Info        int
}

// Stats counts the commands, flags and notes in r. Builtin commands and flags count too.
func (r *Result) Stats() Stats {
	var s Stats
	var walk func(c *Command)
	walk = func(c *Command) {
		s.Commands++
		s.Flags += len(c.Flags)
		for _, sub := range c.Commands {
			walk(sub)
		}
	}
	if r.Command != nil {
		walk(r.Command)
	}
	for _, n := range r.Notes {
		switch n.Level {
		case Lossy:
			s.Lossy++
		case Unsupported:
			s.Unsupported++
		case Info:
			s.Info++
		}
	}
	return s
}

// String renders the counts as the import's summary line:
// "imported 13 commands, 41 flags: 9 lossy, 2 unsupported, 7 info".
func (s Stats) String() string {
	return fmt.Sprintf("imported %d commands, %d flags: %d lossy, %d unsupported, %d info",
		s.Commands, s.Flags, s.Lossy, s.Unsupported, s.Info)
}
