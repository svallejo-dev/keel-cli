// Command keel is the CLI entrypoint for the local Infra-as-a-Service platform.
package main

import (
	"os"

	"github.com/svallejo-dev/keel-cli/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
