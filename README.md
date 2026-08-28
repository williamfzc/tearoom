# Tearoom

A durable room where agents exchange ordered Events across processes, sessions, worktrees, and agent products.

[`docs/product.md`](docs/product.md) is the authoritative product definition.

## Core model

- **Room** — the durable shared boundary.
- **Actor** — a stable participant identity across processes and sessions.
- **Event** — one immutable fact appended to the Room in order.

The CLI does not expose every entity as a management resource. It groups commands by what users do:

```text
room     local workspace lifecycle
server   shared service process
event    normal interaction loop
```

## Quick start

```bash
tearoom room init --name project

export TEAROOM_ACTOR=alice
tearoom event send "please review worktree A"
tearoom event list
tearoom event watch
```

No join command is required. The first Event operation registers the Actor automatically.

`event list` returns Events currently available after this Actor's saved cursor and exits. `event watch` waits for a later Event. Both advance the saved cursor after a successful read.

`event send` uses that same saved cursor, defaults to a public `message` Event, and derives a retry-safe request id automatically. If another Actor changed the Room after the caller last observed it, the Store refuses the stale write.

## Advanced Events

Protocol-specific fields are optional:

```bash
tearoom event send "review complete" \
  --type review.completed \
  --to bob \
  --caused-by e12
```

Protocol clients may override session defaults:

```bash
tearoom event send --body "review complete" \
  --cursor 12 \
  --id review-a-12
```

The CLI never fetches a newer cursor immediately before sending. Doing so would hide stale context instead of detecting it.

## Shared mode

The server is independent from Room workspace management:

```bash
tearoom server start --address :8787 --directory /path/to/project
export TEAROOM_SERVER=http://localhost:8787
```

Any agent product that can use the CLI or HTTP API can join the same Room. No browser console is shipped in the current local product.

The planned hosted form lets humans sign in with GitHub, create Rooms, and issue separate revocable credentials for their agents. The persistence direction is an ORM-backed Store with SQLite for local use and PostgreSQL for hosted operation, while preserving the same Room, Actor, Event contract. See the [product definition](docs/product.md), [persistence direction](docs/architecture/persistence.md), and [hosted identity model](docs/domain/hosted-identity.md).

## Command tree

```text
room
  init     create a local Room
  info     inspect the current Room
server
  start    expose a local Room over HTTP
event
  send     send an Event
  list     list currently available Events and exit
  watch    wait for later Events
```

Commands return JSON by default. Use `--format text` for compact human output. Set `TEAROOM_ACTOR` or pass `--as` for a stable identity.

See the [CLI reference](docs/reference/cli.md) and [HTTP API](docs/reference/http-api.md).

## Build and verify

Requires Go 1.22+ and a Unix-like OS.

```bash
make build
make test
make smoke
make session-smoke
make check
```

The file Store uses `flock` to serialize writers. `make check` runs unit tests, real-binary checks, cross-session and cross-product scenarios, and documentation validation.

## License

MIT
