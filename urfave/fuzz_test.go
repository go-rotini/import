package urfave_test

import (
	"regexp"
	"strings"
	"testing"

	imp "github.com/go-rotini/import"
	"github.com/go-rotini/import/internal/walk"
	urfaveimport "github.com/go-rotini/import/urfave"
	"github.com/urfave/cli/v3"
)

var argName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// FuzzFromURFave checks that any command, alias, flag and usage text imports without error
// into valid rotini names and identifiers.
func FuzzFromURFave(f *testing.F) {
	f.Add("deploy", "d", "replicas", "r", "<service> [revision]")
	f.Add("a.b", "bad alias", "db.host", "1", "<src>... <dst>")
	f.Add("", "", "", "", "")
	f.Fuzz(func(t *testing.T, name, alias, flag, short, usage string) {
		root := &cli.Command{
			Name:     "app",
			Commands: []*cli.Command{{Name: name, Aliases: []string{alias}, ArgsUsage: usage, Action: action}},
			Flags:    []cli.Flag{&cli.StringFlag{Name: flag, Aliases: []string{short}}},
		}
		res, err := urfaveimport.FromURFave(root, urfaveimport.Options{})
		if err != nil {
			t.Fatal(err)
		}
		var check func(c *imp.Command)
		check = func(c *imp.Command) {
			for _, n := range append([]string{c.Name}, c.Aliases...) {
				if !walk.ValidName(n) {
					t.Fatalf("invalid command name %q", n)
				}
			}
			for _, fl := range c.Flags {
				if !walk.ValidName(fl.Name) || len(fl.Identifiers) == 0 {
					t.Fatalf("invalid flag %+v", fl)
				}
				for _, id := range fl.Identifiers {
					if !walk.ValidName(strings.TrimLeft(id, "-")) {
						t.Fatalf("invalid identifier %q", id)
					}
				}
			}
			for _, a := range c.Arguments {
				if !argName.MatchString(a.Name) {
					t.Fatalf("invalid argument name %q", a.Name)
				}
			}
			for _, sub := range c.Commands {
				check(sub)
			}
		}
		check(res.Command)
	})
}
