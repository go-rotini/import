# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability, please report it responsibly by emailing **matthewcgetz@gmail.com**. Do not open a public issue.

You should receive a response within 72 hours. If accepted, a fix will be developed privately and released as a patch version.

## What an import runs

An importer runs inside the program it reads: `rotini import cobra` builds the program's package as a test binary and calls the importer from it. That runs the package's `init` functions and the code that builds its command tree, with your environment, exactly as running the program would. Import only programs you would run.

The importer itself never runs a command, a run hook or a completion function, with two narrow exceptions: it calls a command's argument validator with placeholder arguments to find how many it accepts, and it calls Cobra's `FixedCompletions` functions to list their values. A panic in either is recovered.

The importer writes nothing but the description, to the file it is given. It does not touch the program's source files, `go.mod` or `go.sum`.
