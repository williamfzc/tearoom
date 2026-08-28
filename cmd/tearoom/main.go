// Command tearoom is the single entry point for local and shared rooms.
package main

import (
	"os"

	"github.com/williamfzc/tearoom/internal/cli"
)

func main() {
	os.Exit(cli.New().Run(os.Args[1:]))
}
