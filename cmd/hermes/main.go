package main

import (
	"os"

	"github.com/hermes-notifications/hermes/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
