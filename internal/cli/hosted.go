// The hosted command group: create and manage the control-plane database,
// then serve the multi-tenant event API. Client interaction with a hosted
// server needs no new surface — TEAROOM_SERVER points at /r/<room-id> and
// TEAROOM_TOKEN carries the credential.
package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/williamfzc/tearoom/internal/control"
	"github.com/williamfzc/tearoom/internal/hosted"
	"github.com/williamfzc/tearoom/internal/relational"
	"gorm.io/gorm"
)

const hostedHelp = `Run the hosted control plane and event service.

Usage:
  tearoom hosted <command> [flags]

Commands:
  init-db            create or upgrade the control and coordination schema
  create-room        create a hosted room owned by the dev admin account
  issue-credential   issue one agent credential and print its token once
  revoke             revoke a credential
  serve              serve the hosted event API and management routes

Client side needs no new commands: TEAROOM_SERVER points at /r/<room-id>
and TEAROOM_TOKEN carries a credential.

Environment:
  TEAROOM_HOSTED_DEV    "1" turns on dev administration
  TEAROOM_HOSTED_ADMIN  the dev admin bearer token for management routes
`

func (a *App) runHosted(args []string) error {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprint(a.Out, hostedHelp)
		return nil
	}
	switch args[0] {
	case "init-db":
		return a.hostedInitDB(args[1:])
	case "create-room":
		return a.hostedCreateRoom(args[1:])
	case "issue-credential":
		return a.hostedIssueCredential(args[1:])
	case "revoke":
		return a.hostedRevoke(args[1:])
	case "serve":
		return a.hostedServe(args[1:])
	default:
		return fmt.Errorf("unknown hosted command %q", args[0])
	}
}

func (a *App) hostedInitDB(args []string) error {
	const help = `Create or upgrade the control and coordination schema.

Usage:
  tearoom hosted init-db --database <dsn>

Flags:
  --database string   SQLite path or postgres:// DSN
  -h, --help          show help
`
	flags := newFlags("hosted init-db", a.Out, help)
	database := flags.String("database", "", "")
	if err := parseNoArgs(flags, args); err != nil {
		return err
	}
	db, err := openHostedDB(*database)
	if err != nil {
		return err
	}
	if err := migrateHosted(db); err != nil {
		return err
	}
	fmt.Fprintln(a.Out, "control and coordination schema ready")
	return nil
}

func (a *App) hostedCreateRoom(args []string) error {
	const help = `Create a hosted room owned by the dev admin account.

Usage:
  tearoom hosted create-room --database <dsn> --name <name>

Flags:
  --database string   SQLite path or postgres:// DSN
  --name string       room name
  -h, --help          show help
`
	flags := newFlags("hosted create-room", a.Out, help)
	database := flags.String("database", "", "")
	name := flags.String("name", "", "")
	if err := parseNoArgs(flags, args); err != nil {
		return err
	}
	db, err := openHostedDB(*database)
	if err != nil {
		return err
	}
	account, err := control.DevLogin(context.Background(), db)
	if err != nil {
		return err
	}
	roomID, err := control.CreateRoom(context.Background(), db, account.ID, *name)
	if err != nil {
		return err
	}
	return output(a.Out, map[string]any{"id": roomID, "name": *name}, "json",
		fmt.Sprintf("room %s created with id %s\n", *name, roomID))
}

func (a *App) hostedIssueCredential(args []string) error {
	const help = `Issue one agent credential and print its token.

The token is shown exactly once; only its hash is stored.

Usage:
  tearoom hosted issue-credential --database <dsn> --room <id> \
      --actor <name> --capabilities read,append [--ttl 30d]

Flags:
  --database string       SQLite path or postgres:// DSN
  --room string           hosted room id
  --actor string          actor the credential is bound to
  --capabilities string   comma-joined read,append,manage
  --ttl duration          expiry; 0 means never (default 0)
  -h, --help              show help
`
	flags := newFlags("hosted issue-credential", a.Out, help)
	database := flags.String("database", "", "")
	room := flags.String("room", "", "")
	actor := flags.String("actor", "", "")
	caps := flags.String("capabilities", "", "")
	ttl := flags.Duration("ttl", 0, "")
	if err := parseNoArgs(flags, args); err != nil {
		return err
	}
	db, err := openHostedDB(*database)
	if err != nil {
		return err
	}
	token, cred, err := control.Issue(context.Background(), db, *room, *actor, *caps, *ttl)
	if err != nil {
		return err
	}
	return output(a.Out, map[string]any{
		"id": cred.ID, "token": token, "room": cred.RoomID,
		"actor": cred.ActorID, "capabilities": cred.Capabilities,
		"expires_at": cred.ExpiresAt,
	}, "json", fmt.Sprintf("credential %d for %s issued\n", cred.ID, cred.ActorID))
}

func (a *App) hostedRevoke(args []string) error {
	const help = `Revoke a credential.

Usage:
  tearoom hosted revoke --database <dsn> --room <id> --credential <id>

Flags:
  --database string     SQLite path or postgres:// DSN
  --room string         hosted room id
  --credential uint     credential id to revoke
  -h, --help            show help
`
	flags := newFlags("hosted revoke", a.Out, help)
	database := flags.String("database", "", "")
	room := flags.String("room", "", "")
	credential := flags.Uint64("credential", 0, "")
	if err := parseNoArgs(flags, args); err != nil {
		return err
	}
	if *credential == 0 {
		return errors.New("--credential is required")
	}
	db, err := openHostedDB(*database)
	if err != nil {
		return err
	}
	if err := control.Revoke(context.Background(), db, *room, uint(*credential)); err != nil {
		return err
	}
	fmt.Fprintf(a.Out, "credential %d revoked\n", *credential)
	return nil
}

func (a *App) hostedServe(args []string) error {
	const help = `Serve the hosted event API and management routes.

Management routes (/api/hosted/*) require TEAROOM_HOSTED_DEV=1 and
TEAROOM_HOSTED_ADMIN. The event API (/r/<room-id>/api/*) is always live and
authenticates Bearer credentials.

Usage:
  tearoom hosted serve --database <dsn> [--address :8787]

Flags:
  --database string   SQLite path or postgres:// DSN
  --address string    listen address (default ":8787")
  -h, --help          show help
`
	flags := newFlags("hosted serve", a.Out, help)
	database := flags.String("database", "", "")
	address := flags.String("address", ":8787", "")
	if err := parseNoArgs(flags, args); err != nil {
		return err
	}
	db, err := openHostedDB(*database)
	if err != nil {
		return err
	}
	if err := migrateHosted(db); err != nil {
		return err
	}
	server := &http.Server{
		Addr:              *address,
		Handler:           hosted.New(db, control.DevAdmin()).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	fmt.Fprintf(a.Err, "tearoom hosted serving on %s (database %s)\n", *address, *database)
	if control.DevAdmin() == "" {
		fmt.Fprintln(a.Err, "warning: TEAROOM_HOSTED_DEV is not set; management routes are disabled")
	}
	return server.ListenAndServe()
}

func openHostedDB(database string) (*gorm.DB, error) {
	if strings.TrimSpace(database) == "" {
		return nil, errors.New("--database is required: a SQLite path or a postgres:// DSN")
	}
	return relational.OpenURL(database)
}

// migrateHosted upgrades a shared database to both schemas: the control
// plane (accounts, memberships, credentials) and the coordination schema
// (rooms, actors, events). Order does not matter; they are independent.
func migrateHosted(db *gorm.DB) error {
	if err := relational.Migrate(db); err != nil {
		return err
	}
	return control.Migrate(db)
}
