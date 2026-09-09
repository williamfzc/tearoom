---
type: reference
title: CLI reference
description: Authoritative shipped and target command contract for the tearoom binary.
resource: internal/cli/cli.go
tags: [reference, cli, contract]
timestamp: 2026-09-10
---

# CLI reference

The CLI is Tearoom's top-level product interface. This page is its only command
contract. Every surface is labelled **shipped** or **target** so future behavior
can be designed without being mistaken for current behavior. Implementation
gaps belong in [status.md](../architecture/status.md), and complete user
journeys belong in [evaluations.md](../evaluations.md).

## Shipped command contract

```text
tearoom room init [--as <actor>]
tearoom room join <actor>

tearoom send <text> [--as <actor>] [--idempotency-key <key>]
tearoom send --json <json> [--as <actor>] [--idempotency-key <key>]

tearoom read [--as <actor>] [--after e<seq>]

tearoom server start [--address 127.0.0.1:8787]
tearoom skill
```

### Room lifecycle

- `room init` atomically creates a local Room in the current directory and
  admits its creator. The Room identity comes from the directory that owns
  `.tearoom`; there is no `--name` flag and no backend flag: the Room is one
  SQLite database.
- `room join <actor>` admits the named Actor. The Actor is the positional
  argument: this command does not use `--as` or `TEAROOM_ACTOR`. Repeating the
  command for an admitted Actor succeeds and returns the existing Actor.

Successful `room init` and `room join` calls print one JSON object:

```json
{"id":"room-id","created":"2026-09-09T10:00:00Z"}
{"id":"alice","joined":"2026-09-09T10:00:00Z"}
```

The identifiers and timestamps above are examples, not fixed values.

### Append and read

- `send` appends one Event and prints it as one JSON object. Plain text becomes
  a JSON string; `--json` accepts one valid JSON value.
- `--idempotency-key` is optional. Repeating the same Actor, key, and content
  returns the original Event. Reusing that key with different content fails.
- `read` prints JSONL: one Event per line, with no envelope and no saved cursor.
  An empty result prints nothing and succeeds. `--after e42` starts after that
  exclusive boundary. The read is unbounded and pure: a caller that truncates
  or polls for later Events owns that loop and its boundary.

An Event has this output shape:

```json
{"seq":43,"actor":"alice","at":"2026-09-09T10:01:00Z","content":{"plan":"review"}}
```

`content` is always valid JSON.

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

### Agent guide

`tearoom skill` prints the orientation guide carried by the binary: the Room
journey (setup, `send`, `read`), the environment conventions, and the rules
that hold at every command — pure repeatable reads, caller-owned boundaries,
membership visibility, and idempotent retries. It takes no flags, reads no
Room, and works before any setup, so an Agent that discovers only the binary
can onboard itself without a distributed skill file.

The guide teaches the journey, not the syntax: this page and per-command
`--help` remain the only authority for flags and output shapes. When the
guide and a command disagree, the command wins and the guide is a bug.

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
URL. The remote deployment uses PostgreSQL; the local Room is always SQLite.

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
tearoom room credential issue --actor <actor> --expires <duration>
tearoom room credential revoke <credential-id>
```

These owner operations use the selected Room. Issuing a credential requires an
already-admitted Actor and an expiry. It prints the plaintext token once:

```json
{"id":"credential-id","actor":"bob","expires_at":"2026-10-09T10:00:00Z","token":"tr_example"}
```

A credential authorizes writes as its Actor; reading requires no credential —
the Room URL is the read capability. An Agent uses exactly one Room URL,
stable Actor identity, and its credential for sends:

```bash
export TEAROOM_SERVER=https://tearoom.example/r/room-id
export TEAROOM_ACTOR=bob
export TEAROOM_TOKEN=tr_example
tearoom read
tearoom send "review complete"
```

The credential is bound to its Room, Actor, and expiry. An Actor mismatch,
expiry, or revocation fails without changing the Room, Actor, or history.
Rotation is issue-new then revoke-old; it needs no separate command. The
target loopback profile uses the same authorization decisions but bootstraps
its local owner without login ceremony or tokens.

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
In particular, an empty `read` succeeds, and a missing local Room or unreachable
configured server is an availability failure.

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
