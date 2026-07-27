package main

import (
	"context"
	"os"

	"github.com/rafflesia-ai/molstar/internal/cli"
)

func main() {
	if err := cli.Execute(context.Background(), os.Args[1:]); err != nil {
		os.Exit(cli.ExitCode(err))
	}
}
