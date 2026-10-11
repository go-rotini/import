package urfave_test

import (
	"context"
	"fmt"

	urfaveimport "github.com/go-rotini/import/urfave"
	"github.com/urfave/cli/v3"
)

func ExampleFromURFave() {
	root := &cli.Command{
		Name:  "acme",
		Usage: "Acme ops tool",
		Commands: []*cli.Command{{
			Name:      "deploy",
			Usage:     "Deploy a service",
			ArgsUsage: "<service>",
			Flags:     []cli.Flag{&cli.IntFlag{Name: "replicas", Aliases: []string{"r"}, Value: 1, Usage: "replica count"}},
			Action:    func(context.Context, *cli.Command) error { return nil },
		}},
	}
	res, err := urfaveimport.FromURFave(root, urfaveimport.Options{Strict: true})
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, c := range res.Command.Commands {
		fmt.Printf("%s builtin=%q\n", c.Name, c.Builtin)
	}
	d := res.Command.Commands[0]
	fmt.Println(d.Arguments[0].Name, d.Flags[0].Identifiers, d.Flags[0].Schema.Default)
	fmt.Println(res.Hooks[0].Path, res.Hooks[0].Framework, "->", res.Hooks[0].Rotini)
	// Output:
	// deploy builtin=""
	// help builtin="help"
	// service [-r --replicas] 1
	// acme deploy Action -> Run
}
