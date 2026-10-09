# Contributing

Contributions are welcome! Here's how to get started.

## Setup

```bash
git clone https://github.com/go-rotini/import.git
cd import
go work init . ./cobra   # develop the root module and the importers together
make all                 # the root module
make -C cobra all        # the Cobra importer
```

The repository holds several modules: the root module and one nested module per framework. A `go.work` (ignored by git) lets a change to the root module reach the importers before it is released.

## Making Changes

1. Fork the repository and create a branch from `main`.
2. Write tests for any new functionality.
3. Ensure `make all` passes in every module you touched before submitting a pull request.
4. Use [Conventional Commits](https://www.conventionalcommits.org/) for commit messages (e.g., `feat:`, `fix:`, `test:`, `docs:`).

## Linting

```bash
make lint
make -C cobra lint
```

## Testing

```bash
make test                       # the root module's tests
make -C cobra test              # the Cobra importer's tests, including the offline corpus
make -C cobra test-cobra-versions  # the Cobra importer against every supported Cobra release
make test-fuzz                  # fuzz tests (60s per fuzzer)
make test-race                  # tests with the race detector
ROTINI_IMPORT_NETWORK=1 go -C cobra test -run TestNetworkCorpus .  # import Delve and kubectl
```

The corpus under `cobra/testdata/corpus` holds small Cobra programs, each a module of its own, imported the way the rotini companion CLI imports them. After an intended change to the output, rewrite the golden files with `go -C cobra test -run TestCorpus -update .` and review the diff.

## Pull Requests

- Keep PRs focused on a single change.
- Include tests that cover the change.
- Reference any relevant issues.

## Reporting Bugs

Open an issue with the smallest command tree that shows the problem (the `cobra.Command` declarations, flags and annotations), the note or description you got and the one you expected, and the framework's version.

## Security

See [SECURITY.md](SECURITY.md) for reporting vulnerabilities.
