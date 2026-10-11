// Package imp is the framework-neutral description of a command-line program that the
// go-rotini importers produce and the rotini companion CLI turns into a spec.
//
// An importer (github.com/go-rotini/import/cobra, kong or urfave) walks a live command tree and
// returns a [Result]: the command tree in the shape of a rotini spec, the notes on what
// could not be carried across exactly, and pointers to the code the author has to port by
// hand. [Write] encodes it as JSON; [Read] decodes it and refuses a format it does not know.
//
// The description is versioned by [Format]. A reader accepts any description whose major
// version it knows; new optional fields may appear within a major version.
//
// This package never writes a spec. The rotini companion CLI owns the spec's version, its
// help and version seeds, its comments and its format, so an importer release can never
// produce a spec for a rotini version that does not exist.
package imp
