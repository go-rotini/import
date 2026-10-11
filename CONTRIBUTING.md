# Contributing

Contributions are welcome! Here's how to get started.

## Setup

```bash
git clone https://github.com/go-rotini/import.git
cd import
make all
```

## Making Changes

1. Fork the repository and create a branch from `main`.
2. Write tests for any new functionality.
3. Ensure `make all` passes before submitting a pull request.
4. Use [Conventional Commits](https://www.conventionalcommits.org/) for commit messages (e.g., `feat:`, `fix:`, `test:`, `docs:`).

## Linting

```bash
make lint
```

## Testing

```bash
make test                   # all tests, including each importer's offline corpus
make test-cobra-versions    # the Cobra importer against every supported Cobra release
make test-kong-versions     # the Kong importer against every supported Kong release
make test-urfave-versions   # the urfave/cli importer against every supported urfave/cli release
make test-fuzz              # fuzz tests (60s per fuzzer)
make test-race              # tests with the race detector
ROTINI_IMPORT_NETWORK=1 go test -run TestNetworkCorpus ./cobra  # import Delve and kubectl
```

The corpus under each importer's `testdata/corpus` holds small programs, each a module of its own, imported the way the rotini companion CLI imports them. After an intended change to the output, rewrite the golden files with `go test -run TestCorpus ./cobra ./kong ./urfave -update` and review the diff.

## Pull Requests

- Keep PRs focused on a single change.
- Include tests that cover the change.
- Reference any relevant issues.

## Reporting Bugs

Open an issue with the smallest command tree that shows the problem (the `cobra.Command` declarations, the Kong grammar or the `cli.Command` tree), the note or description you got and the one you expected, and the framework's version.

## Security

See [SECURITY.md](SECURITY.md) for reporting vulnerabilities.
