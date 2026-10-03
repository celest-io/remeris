// Command remeris is a Go reimplementation of Knope.
package main

import (
	"context"
	"os"

	"github.com/celest-io/remeris/internal/cli"
)

func main() {
	os.Exit(cli.Main(context.Background(), os.Args, os.Stdout, os.Stderr))
}
