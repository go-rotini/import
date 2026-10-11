# go-rotini/import/kong

Reads a [Kong](https://github.com/alecthomas/kong) grammar into the description defined by [go-rotini/import](https://github.com/go-rotini/import), which the [rotini](https://github.com/go-rotini/rotini) companion CLI turns into a spec, a conf and handler stubs (`rotini import kong <pkg>`).

```go
res, err := kongimport.FromKong(&CLI, kongimport.Options{Name: "acme", Vars: map[string]string{"version": "1.0.0"}})
```

Builds against Kong 0.9 and later; the module's `go.mod` requires the oldest, so it never raises the Kong version of the program it reads. Kong's model is built with `kong.New`, so options the program passes to `kong.Parse` in `main` are not seen unless given in `Options`: `Name` (Kong otherwise names the root after the test binary), `Vars` (a `${name}` left out is reported and replaced) and `KongOptions` for any other.

See the [repository README](https://github.com/go-rotini/import#readme) for what carries across and what is reported.
