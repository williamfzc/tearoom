// server.go runs the HTTP profiles: the local loopback server over the
// nearest Room, and the hosted server over a shared deployment database.
package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/hosted"
	"github.com/williamfzc/tearoom/internal/relational"
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
	const help = `Serve a Room over HTTP.

Local profile: serves the nearest Room and binds the loopback interface
only, because the local profile has no credentials.

Hosted profile: set TEAROOM_DSN to a deployment database (postgres://... or
a SQLite file) and TEAROOM_OWNER_TOKEN to the deployment secret. The hosted
service hosts many rooms behind /r/<id> URLs, may bind any interface, and
enforces credentials on writes while reads stay open.

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
	if dsn := os.Getenv("TEAROOM_DSN"); dsn != "" {
		return a.serveHosted(*address, dsn)
	}
	if err := requireLoopback(*address); err != nil {
		return err
	}
	st, err := store.Open(a.Dir)
	if err != nil {
		return fmt.Errorf("open room under %s: %w", a.Dir, err)
	}
	defer closeStore(st)
	root := a.Dir
	if r, ok := st.(interface{ Root() string }); ok {
		root = r.Root()
	}
	fmt.Fprintf(a.Err, "tearoom serving %s on %s\n", root, *address)
	return a.serve(*address, transport.NewServer(st).Handler())
}

// serveHosted runs the hosted profile: one database, many rooms, credentials
// enforced. The owner token is mandatory because an exposed service without
// credentials is refused by design.
func (a *App) serveHosted(address, dsn string) error {
	owner := os.Getenv("TEAROOM_OWNER_TOKEN")
	if owner == "" {
		return errors.New("hosted mode requires TEAROOM_OWNER_TOKEN")
	}
	db, err := relational.OpenDatabase(dsn)
	if err != nil {
		return fmt.Errorf("open deployment database: %w", err)
	}
	h, err := hosted.New(db, owner, nil)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.Err, "tearoom hosting a shared database on %s\n", address)
	return a.serve(address, h.Handler())
}

// serve runs one HTTP server until the process context is cancelled.
func (a *App) serve(address string, handler http.Handler) error {
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	ctx := a.Context
	if ctx == nil {
		ctx = context.Background()
	}
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
		return fmt.Errorf("refusing listen address %q: P0 serves loopback only; use 127.0.0.1:8787", address)
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	return fmt.Errorf("refusing listen address %q: P0 serves loopback only; use 127.0.0.1:8787", address)
}
