package main

import (
	"fmt"
	"os"

	"github.com/codegangsta/cli"
	. "github.com/tendermint/tendermint/Godeps/_workspace/src/github.com/tendermint/go-common"
	"github.com/tendermint/tendermint/Godeps/_workspace/src/github.com/tendermint/go-logio"
)

func main() {
	app := cli.NewApp()
	app.Name = "logio"
	app.Usage = "Utilities for log entries created by go-logio"
	app.Commands = []cli.Command{
		{
			Name:   "print",
			Usage:  "Print log entries",
			Action: cmdLogioPrint,
		},
	}
	app.Run(os.Args)

}

func cmdLogioPrint(c *cli.Context) {
	if len(c.Args()) == 0 {
		Exit("logio requires a file to print")
	}

	// TODO: handle multiple files as created by logjack.
	path := c.Args()[0]
	fmt.Println("Printing log entries from path:", path)
	logio.PrintFile(path)
}
