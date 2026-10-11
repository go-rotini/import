# go-rotini/import/urfave

Reads a live [urfave/cli v3](https://github.com/urfave/cli) command tree into the description defined by [go-rotini/import](https://github.com/go-rotini/import), which the [rotini](https://github.com/go-rotini/rotini) companion CLI turns into a spec, a conf and handler stubs (`rotini import urfave <pkg>`).

```go
res, err := urfaveimport.FromURFave(newApp(), urfaveimport.Options{})
```

Builds against urfave/cli 3.1 and later; the module's `go.mod` requires the oldest, so it never raises the urfave/cli version of the program it reads. Fields that later releases added (`Deprecated`, `StopOnNthArg`, `ArgValidator` and others) are read when the release in use has them.

See the [repository README](https://github.com/go-rotini/import#readme) for what carries across and what is reported.
