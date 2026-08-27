// Command tea is the agent-facing CLI.
package main

import (
	"os"

	"github.com/williamfzc/tearoom/internal/cli"
)

func main() {
	os.Exit(cli.New().Run(os.Args[1:]))
}
