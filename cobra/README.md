# go-rotini/import/cobra

Reads a live [Cobra](https://github.com/spf13/cobra) command tree into the description defined by [go-rotini/import](https://github.com/go-rotini/import), which the [rotini](https://github.com/go-rotini/rotini) companion CLI turns into a spec, a conf and handler stubs (`rotini import cobra <pkg>`).

```go
res, err := cobraimport.FromCobra(rootCmd, cobraimport.Options{})
```

Builds against Cobra 1.6 and later; its `go.mod` requires the oldest, so it never raises the Cobra version of the program it reads. Flag completion functions are visible from Cobra 1.8 on.

See the [repository README](https://github.com/go-rotini/import#readme) for what carries across and what is reported.
