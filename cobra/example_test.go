package cobra_test

import (
	"fmt"

	cobraimport "github.com/go-rotini/import/cobra"
	"github.com/spf13/cobra"
)

func ExampleFromCobra() {
	root := &cobra.Command{Use: "acme", Short: "Acme ops tool"}
	deploy := &cobra.Command{
		Use:   "deploy <service>",
		Short: "Deploy a service",
		Args:  cobra.ExactArgs(1),
		Run:   func(*cobra.Command, []string) {},
	}
	deploy.Flags().IntP("replicas", "r", 1, "replica count")
	root.AddCommand(deploy)

	res, err := cobraimport.FromCobra(root, cobraimport.Options{})
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, c := range res.Command.Commands {
		fmt.Printf("%s builtin=%q\n", c.Name, c.Builtin)
	}
	d := res.Command.Commands[1]
	fmt.Println(d.Arguments[0].Name, d.Flags[0].Identifiers, d.Flags[0].Schema.Default)
	fmt.Println(res.Hooks[0].Path, res.Hooks[0].Framework, "->", res.Hooks[0].Rotini)
	// Output:
	// completion builtin="completion"
	// deploy builtin=""
	// help builtin="help"
	// service [-r --replicas] 1
	// acme deploy Run -> Run
}
