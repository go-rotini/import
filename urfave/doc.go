// Package urfave reads a urfave/cli v3 command tree into the framework-neutral description
// of github.com/go-rotini/import, which the rotini companion CLI turns into a spec, a conf
// and handler stubs.
//
// [FromURFave] walks the tree a program has built, so it sees exactly what the program
// declares: commands, flags, arguments, value sources and mutually exclusive groups. It
// never runs a command. urfave/cli adds its help and version flags and its help and
// completion commands only while running, so the import adds them as urfave/cli would and
// marks them as builtin.
//
// What cannot be carried across exactly is reported as notes, and the functions that run at
// each lifecycle step (Before, Action, After) are reported as hooks with their file and
// line, for porting by hand.
package urfave
