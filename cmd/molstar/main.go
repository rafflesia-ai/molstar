package main

import (
	"context"
	"os"

	"github.com/sacha-ichbiah/molstar/internal/cli"
)

func main() {
	if err := cli.Execute(context.Background(), os.Args[1:]); err != nil {
		os.Exit(cli.ExitCode(err))
	}
}
