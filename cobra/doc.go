// Package cobra reads a live Cobra command tree into the framework-neutral description of
// github.com/go-rotini/import, which the rotini companion CLI turns into a spec, a conf and
// handler stubs.
//
// [FromCobra] walks the tree a program has built, so it sees exactly what the program
// declares: commands, flags, annotations and argument validators. It never runs a command.
// Before walking it asks Cobra to add the defaults Cobra otherwise adds only while executing
// (the help and completion commands, and the help and version flags), so they are imported
// too and marked as builtin.
//
// What cannot be carried across exactly is reported as notes, and the functions that run at
// each lifecycle step are reported as hooks with their file and line, for porting by hand.
//
// Flags or commands a program registers lazily (inside a run hook, an initializer passed to
// cobra.OnInitialize, a plugin loader or a wrapper around Execute) are not in the tree when
// it is walked, so they are not imported.
package cobra
