package kong_test

import (
	"fmt"

	kongimport "github.com/go-rotini/import/kong"
)

type deployCmd struct {
	Service  string `arg:"" help:"Service to deploy."`
	Replicas int    `short:"r" default:"1" help:"Replica count."`
}

func (d *deployCmd) Run() error { return nil }

func ExampleFromKong() {
	var cli struct {
		Deploy deployCmd `cmd:"" help:"Deploy a service."`
	}
	res, err := kongimport.FromKong(&cli, kongimport.Options{Name: "acme"})
	if err != nil {
		fmt.Println(err)
		return
	}
	d := res.Command.Commands[0]
	fmt.Println(res.Command.Flags[0].Identifiers, res.Command.Flags[0].Builtin)
	fmt.Println(d.Name, d.Arguments[0].Name, d.Flags[0].Identifiers, d.Flags[0].Schema.Default)
	fmt.Println(res.Hooks[0].Path, res.Hooks[0].Framework, "->", res.Hooks[0].Rotini)
	// Output:
	// [-h --help] help
	// deploy service [-r --replicas] 1
	// acme deploy Run -> Run
}
