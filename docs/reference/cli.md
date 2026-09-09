---
type: reference
title: CLI reference
description: Authoritative shipped and target command contract for the tearoom binary.
resource: internal/cli/cli.go
tags: [reference, cli, contract]
timestamp: 2026-09-09
---

# CLI reference

The CLI is Tearoom's top-level product interface. This page is its only command
contract. Every surface is labelled **shipped** or **target** so future behavior
can be designed without being mistaken for current behavior. Implementation
gaps belong in [status.md](../architecture/status.md), and complete user
journeys belong in [evaluations.md](../evaluations.md).

## Shipped command contract

```text
tearoom room init [--as <actor>] [--store file|sqlite]
tearoom room join <actor>
tearoom room members

tearoom send <text> [--as <actor>] [--to <member>]... [--idempotency-key <key>]
tearoom send --json <json> [--as <actor>] [--to <member>]... [--idempotency-key <key>]

tearoom read [--as <actor>] [--after e<seq>] [--limit N] [--wait <duration>]

tearoom server start [--address 127.0.0.1:8787]
```

### Room lifecycle

- `room init` atomically creates a local Room in the current directory and
  admits its creator. The Room identity comes from the directory that owns
  `.tearoom`; there is no `--name` flag. `--store` selects `file` (the current
  default) or `sqlite`.
- `room join <actor>` admits the named Actor. The Actor is the positional
  argument: this command does not use `--as` or `TEAROOM_ACTOR`. Repeating the
  command for an admitted Actor succeeds and returns the existing Actor.
- `room members` lists the admitted Actors and requires no Actor identity.

Successful `room init` and `room join` calls print one JSON object; `room
members` prints one JSON array:

```json
{"id":"room-id","created":"2026-09-09T10:00:00Z"}
{"id":"alice","joined":"2026-09-09T10:00:00Z"}
[{"id":"alice","joined":"2026-09-09T10:00:00Z"}]
```

The identifiers and timestamps above are examples, not fixed values.

### Append and read

- `send` appends one Event and prints it as one JSON object. Plain text becomes
  a JSON string; `--json` accepts one valid JSON value. `--to` is repeatable,
  public addressing metadata and never a visibility boundary.
- `--idempotency-key` is optional. Repeating the same Actor, key, addressees,
  and content returns the original Event. Reusing that key with different
  addressees or content fails.
- `read` prints JSONL: one Event per line, with no envelope and no saved cursor.
  An empty result prints nothing and succeeds. `--after e42` starts after that
  exclusive boundary; `--limit` caps the result.
- `read --wait 5m` holds only the current invocation. It returns when an Event
  after the chosen boundary exists and times out with no output when the bound
  elapses. It creates no subscription or durable delivery state.

An Event has this output shape:

```json
{"seq":43,"actor":"alice","at":"2026-09-09T10:01:00Z","to":["bob"],"content":{"plan":"review"}}
```

`to` is omitted when empty. `content` is always valid JSON.

### Actor selection

`room init`, `send`, and `read` select the acting identity from `--as`, falling
back to `TEAROOM_ACTOR`. They fail if neither is set. `room join` instead takes
the Actor being admitted as its positional argument, and `room members` needs
no acting identity in the shipped local trust profile.

### Local server target

`server start` serves the nearest Room over HTTP. The shipped command refuses
non-loopback addresses because this profile has no authentication and must not
be exposed to a network.

When `TEAROOM_SERVER` contains that server URL, `room join`, `room members`,
`send`, and `read` use the [HTTP contract](http-api.md) instead of opening the
nearest local Room. `room init` is local and refuses to run while
`TEAROOM_SERVER` is set.

```bash
# Terminal 1, inside an initialized Room
tearoom server start

# Terminal 2
export TEAROOM_SERVER=http://127.0.0.1:8787
tearoom room members
tearoom send "ready" --as alice
tearoom read --as alice
```

## Target remote profile (not shipped)

The target preserves the same `room`, `send`, and `read` vocabulary remotely.
PostgreSQL is selected by the remote deployment, never by an Agent-side
`--store postgres` flag. The commands in this section are product decisions for
the next implementation; invoking them against the current binary fails.

### Human login and remote Room creation

```text
tearoom auth login --server <service-url>
tearoom room init --server <service-url> --as <owner>
```

`auth login` establishes the human owner session. `room init --server` creates
a Room through that same service and prints the selected, Room-specific server
URL. The remote deployment uses PostgreSQL; the local form continues to use
the backend selected by `--store`.

```json
{"account":"alice","server":"https://tearoom.example"}
```

```json
{"id":"room-id","created":"2026-09-09T10:00:00Z","server":"https://tearoom.example/r/room-id"}
```

`TEAROOM_SERVER` always names one selected Room for subsequent commands. There
is no separate Room-ID environment variable.

### Per-Agent credentials

```text
tearoom room credential issue --actor <actor> --scope read,send --expires <duration>
tearoom room credential revoke <credential-id>
```

These owner operations use the selected Room. Issuing a credential requires an
already-admitted Actor, explicit scopes, and an expiry. It prints the plaintext
token once:

```json
{"id":"credential-id","actor":"bob","scope":["read","send"],"expires_at":"2026-10-09T10:00:00Z","token":"tr_example"}
```

An Agent then uses exactly one Room URL, stable Actor identity, and credential:

```bash
export TEAROOM_SERVER=https://tearoom.example/r/room-id
export TEAROOM_ACTOR=bob
export TEAROOM_TOKEN=tr_example
tearoom read
tearoom send "review complete"
```

The credential is bound to its Room, Actor, scopes, and expiry. An Actor
mismatch, missing scope, expiry, or revocation fails without changing the
Room, Actor, or history. Rotation is issue-new then revoke-old; it needs no
separate command. The target loopback profile uses the same authorization
decisions but may bootstrap its local owner without login ceremony.

### Remote read/write loop

There is no share, export, snapshot, link, or continuation command. An operator
gives each receiving Agent its Room URL, Actor identity, and credential through
an external secure channel. The Agent pulls JSONL and writes back through the
ordinary commands:

```bash
export TEAROOM_SERVER=https://tearoom.example/r/room-id
export TEAROOM_ACTOR=bob
export TEAROOM_TOKEN=tr_example
tearoom read --after e42
tearoom send --json '{"result":"continued"}'
```

A later person or Agent repeats `read` from its own boundary. Everyone
continues in the same Room unless an operator explicitly creates another Room;
Tearoom does not fork collaboration implicitly.

## Failure contract

All diagnostics go to standard error; successful machine-readable output goes
to standard output. [exit-codes.md](exit-codes.md) is the only exit-code source.
In particular, an empty `read` succeeds, a bounded wait with no later Event is
a timeout, and a missing local Room or unreachable configured server is an
availability failure.

The target remote commands keep those same classes. Missing, expired, revoked,
wrong-Actor, or insufficient-scope credentials are ordinary request failures;
they never masquerade as an empty successful read. Remote login and Room
creation errors use the same request-failure class until a shipped use case
proves a separate machine action is necessary.

## Delivery boundary

The CLI does not wake offline Actors, and no such service ships in this
repository (adjudicated 2026-09-09). An external operator or Agent product
starts and configures the receiving process, which invokes `read` and `send`.
The Room connection, not Event content, is passed to that process. Neither
`read` nor `send` acquires a hidden read cursor or scheduler responsibility.
