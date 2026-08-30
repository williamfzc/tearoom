// Command tearoom is the single entry point for local and shared rooms.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/williamfzc/tearoom/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app := cli.New()
	app.Context = ctx
	os.Exit(app.Run(os.Args[1:]))
}
