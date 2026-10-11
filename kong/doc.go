// Package kong reads a Kong grammar into the framework-neutral description of
// github.com/go-rotini/import, which the rotini companion CLI turns into a spec, a conf and
// handler stubs.
//
// [FromKong] builds the Kong model from a grammar struct, as the program's own kong.Parse
// does, and walks it: commands, flags, positional arguments, environment variables,
// defaults, enums, and the xor groups and "and" groups. It parses no command line and runs nothing.
// Kong's help flag is imported too and marked as builtin.
//
// What cannot be carried across exactly is reported as notes, and the methods Kong calls at
// each lifecycle step (Run, the Before and After hooks) are reported as hooks with their
// file and line, for porting by hand.
//
// Options the program passes to kong.Parse or kong.New in main are not part of the grammar.
// [Options] supplies the ones that shape the model: the program's name, the ${name}
// variables, and any other kong.Option.
package kong
