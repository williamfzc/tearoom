// server.go runs the local HTTP profile: the loopback server over the
// nearest workspace. There is no hosted profile; exposing the server beyond
// one trusted machine is a misdeployment.
package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/store"
	"github.com/williamfzc/tearoom/internal/transport"
)

func (a *App) runServer(args []string) error {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprint(a.Out, serverHelp)
		return nil
	}
	if args[0] != "start" {
		return fmt.Errorf("unknown server command %q", args[0])
	}
	return a.serverStart(args[1:])
}

func (a *App) serverStart(args []string) error {
	const help = `Serve the nearest workspace over HTTP.

The local profile binds the loopback interface only: it has no credentials,
and everything on the loopback interface belongs to one OS user — the same
trust domain as the .tearoom directory.

Usage:
  tearoom server start [flags]

Flags:
  --address string      listen address (default "127.0.0.1:8787")
  -h, --help            show help
`
	flags := newFlags("server start", a.Out, help)
	address := flags.String("address", "127.0.0.1:8787", "")
	if err := parseNoArgs(flags, args); err != nil {
		return err
	}
	if err := requireLoopback(*address); err != nil {
		return err
	}
	st, err := store.Open(a.Dir)
	if err != nil {
		return fmt.Errorf("open workspace under %s: %w", a.Dir, err)
	}
	defer closeStore(st)
	root := a.Dir
	if r, ok := st.(interface{ Root() string }); ok {
		root = r.Root()
	}
	fmt.Fprintf(a.Err, "tearoom serving %s on %s\n", root, *address)
	return a.serve(*address, transport.NewServer(st).Handler())
}

// serve runs one HTTP server until the process context is cancelled.
func (a *App) serve(address string, handler http.Handler) error {
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	ctx := a.context()
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ListenAndServe() }()
	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}
		if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func closeStore(st core.Store) {
	if closer, ok := st.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}

// requireLoopback rejects any listen address whose host is not a loopback
// address. The default of the old server, ":8787" (all interfaces), is refused.
func requireLoopback(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid listen address %q: %w", address, err)
	}
	if host == "" {
		return fmt.Errorf("refusing listen address %q: the local profile serves loopback only; use 127.0.0.1:8787", address)
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	return fmt.Errorf("refusing listen address %q: the local profile serves loopback only; use 127.0.0.1:8787", address)
}
