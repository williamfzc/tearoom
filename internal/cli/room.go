// room.go is the room surface: create, join, and credentials. Room creation
// is local by default and remote with --server; credentials are owner
// operations over the served contract.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/williamfzc/tearoom/internal/store"
	"github.com/williamfzc/tearoom/internal/transport"
)

const roomHelp = `Manage rooms and their members.

Usage:
  tearoom room init [--as <actor>] [--server <service-url>]
  tearoom room join <actor>
  tearoom room credential issue --actor <actor> --expires <duration>
  tearoom room credential revoke <credential-id>

Local room creation needs no credentials. Remote creation and every
credential operation carry the owner token (TEAROOM_TOKEN) and target the
service or room URL in --server or TEAROOM_SERVER.
`

func (a *App) runRoom(args []string) error {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprint(a.Out, roomHelp)
		return nil
	}
	switch args[0] {
	case "init":
		return a.roomInit(args[1:])
	case "join":
		return a.roomJoin(args[1:])
	case "credential":
		return a.runRoomCredential(args[1:])
	default:
		return fmt.Errorf("unknown room command %q", args[0])
	}
}

// roomInit atomically creates a room and admits the creator: one command, one
// actor. Local creation takes the Room identity from the directory that owns
// .tearoom; --server creates the Room on a hosted service instead and answers
// with the room URL that TEAROOM_SERVER points at from then on.
func (a *App) roomInit(args []string) error {
	const help = `Create a room and admit the creator.

Local (default): the Room lives in .tearoom beside your work.
Remote (--server): the hosted service creates the Room and answers with its
room URL; the owner token comes from TEAROOM_TOKEN.

Usage:
  tearoom room init [--as <actor>] [--server <service-url>]

Flags:
  --as string        actor identity; defaults to TEAROOM_ACTOR
  --server string    create the Room on this hosted service
  -h, --help         show help
`
	flags := newFlags("room init", a.Out, help)
	as := flags.String("as", "", "")
	server := flags.String("server", "", "")
	if err := parseNoArgs(flags, args); err != nil {
		return err
	}
	actor, err := actorID(*as)
	if err != nil {
		return err
	}
	if *server != "" {
		return a.roomInitRemote(strings.TrimRight(*server, "/"), actor)
	}
	if os.Getenv("TEAROOM_SERVER") != "" {
		return errors.New("TEAROOM_SERVER names one existing room; pass --server to create a new one")
	}
	st, err := store.Init(a.Dir, actor)
	if err != nil {
		return err
	}
	room, err := st.Room(context.Background())
	if err != nil {
		return err
	}
	return json.NewEncoder(a.Out).Encode(room)
}

// roomInitRemote creates a Room on a hosted service as the token bearer.
func (a *App) roomInitRemote(base, actor string) error {
	if err := transport.RequireSafeURL(base); err != nil {
		return err
	}
	client := transport.NewClient(base, strings.TrimSpace(os.Getenv("TEAROOM_TOKEN")))
	if ca := strings.TrimSpace(os.Getenv("TEAROOM_CA_FILE")); ca != "" {
		if err := client.UseCAFile(ca); err != nil {
			return err
		}
	}
	room, err := client.CreateRoom(context.Background(), actor)
	if err != nil {
		return err
	}
	return json.NewEncoder(a.Out).Encode(room)
}

func (a *App) runRoomCredential(args []string) error {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprint(a.Out, `Manage write credentials of the selected room.

Usage:
  tearoom room credential issue --actor <actor> --expires <duration>
  tearoom room credential revoke <credential-id>

Both are owner operations: TEAROOM_TOKEN must carry the owner token, and
TEAROOM_SERVER must name the room. Issue prints the plaintext token once.
`)
		return nil
	}
	switch args[0] {
	case "issue":
		return a.credentialIssue(args[1:])
	case "revoke":
		return a.credentialRevoke(args[1:])
	default:
		return fmt.Errorf("unknown room credential command %q", args[0])
	}
}

// credentialIssue prints the plaintext token exactly once.
func (a *App) credentialIssue(args []string) error {
	const help = `Issue a write credential for an admitted actor.

Usage:
  tearoom room credential issue --actor <actor> --expires <duration>

Flags:
  --actor string     the admitted actor this credential writes as
  --expires duration lifetime of the credential, e.g. 720h
  -h, --help         show help
`
	flags := newFlags("room credential issue", a.Out, help)
	actor := flags.String("actor", "", "")
	expires := flags.String("expires", "", "")
	if err := parseNoArgs(flags, args); err != nil {
		return err
	}
	if *actor == "" || *expires == "" {
		return errors.New("both --actor and --expires are required")
	}
	ttl, err := time.ParseDuration(*expires)
	if err != nil {
		return fmt.Errorf("invalid --expires %q: %w", *expires, err)
	}
	st, err := a.openStore()
	if err != nil {
		return err
	}
	issuer, ok := st.(interface {
		IssueCredential(ctx context.Context, request transport.IssueRequest) (transport.IssuedCredential, error)
	})
	if !ok {
		return errors.New("credentials are a served-contract operation; set TEAROOM_SERVER")
	}
	issued, err := issuer.IssueCredential(context.Background(), transport.IssueRequest{Actor: *actor, TTLSeconds: int(ttl / time.Second)})
	if err != nil {
		return err
	}
	return json.NewEncoder(a.Out).Encode(issued)
}

// credentialRevoke invalidates one credential immediately.
func (a *App) credentialRevoke(args []string) error {
	const help = `Revoke one credential.

Usage:
  tearoom room credential revoke <credential-id>
`
	flags := newFlags("room credential revoke", a.Out, help)
	if err := parse(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: tearoom room credential revoke <credential-id>")
	}
	st, err := a.openStore()
	if err != nil {
		return err
	}
	revoker, ok := st.(interface {
		RevokeCredential(ctx context.Context, credentialID string) error
	})
	if !ok {
		return errors.New("credentials are a served-contract operation; set TEAROOM_SERVER")
	}
	return revoker.RevokeCredential(context.Background(), strings.TrimSpace(flags.Arg(0)))
}

// roomJoin is explicit admission within the trust domain of one OS user.
func (a *App) roomJoin(args []string) error {
	const help = `Join the nearest room as an explicit member.

Usage:
  tearoom room join <actor>

Arguments:
  <actor>            stable actor identity to admit

Flags:
  -h, --help         show help
`
	flags := newFlags("room join", a.Out, help)
	if err := parse(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: tearoom room join <actor>")
	}
	actor := strings.TrimSpace(flags.Arg(0))
	if actor == "" {
		return errors.New("actor identity is required")
	}
	st, err := a.openStore()
	if err != nil {
		return err
	}
	admitted, err := st.AdmitActor(context.Background(), actor)
	if err != nil {
		return err
	}
	return json.NewEncoder(a.Out).Encode(admitted)
}
