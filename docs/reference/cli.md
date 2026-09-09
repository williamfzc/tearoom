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
contract. Every surface is labelled **shipped** or **reserved** so future
behavior can be designed without being mistaken for current behavior.
Implementation gaps belong in [status.md](../architecture/status.md), and
complete user journeys belong in [evaluations.md](../evaluations.md).

## Shipped command contract

```text
tearoom room init [--as <actor>] [--server <service-url>]
tearoom room join <actor>
tearoom room credential issue --actor <actor> --expires <duration>
tearoom room credential revoke <credential-id>

tearoom send <text> [--as <actor>] [--idempotency-key <key>]
tearoom send --json <json> [--as <actor>] [--idempotency-key <key>]

tearoom read [--as <actor>] [--after e<seq>]

tearoom server start [--address 127.0.0.1:8787]
tearoom skill
```

### Room lifecycle

- `room init` atomically creates a Room and admits its creator. Local
  (default) creation takes the Room identity from the directory that owns
  `.tearoom`; the Room is one SQLite database, and there is no `--name` or
  backend flag.
- `room init --server <service-url>` creates the Room on a hosted service
  instead. It carries the owner token (`TEAROOM_TOKEN`) and answers with the
  room URL — `{"id":...,"created":...,"server":".../r/<room-id>"}` — that
  `TEAROOM_SERVER` points at from then on. Room identifiers are minted by the
  service.
- `room join <actor>` admits the named Actor. The Actor is the positional
  argument: this command does not use `--as` or `TEAROOM_ACTOR`. Repeating the
  command for an admitted Actor succeeds and returns the existing Actor. On a
  hosted service, joins are owner operations.
- `room credential issue --actor <actor> --expires <duration>` mints one
  write credential for an already-admitted Actor and prints the plaintext
  token exactly once: `{"id":...,"actor":...,"expires_at":...,"token":"tr_..."}`.
- `room credential revoke <credential-id>` invalidates that credential
  immediately. Rotation is issue-new then revoke-old; it needs no separate
  command.

Issue and revoke are owner operations: the bearer in `TEAROOM_TOKEN` must be
the deployment's owner token, and `TEAROOM_SERVER` must name the room.

Successful lifecycle commands print one JSON object:

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

### Actor selection and credentials

`room init`, `send`, and `read` select the acting identity from `--as`, falling
back to `TEAROOM_ACTOR`. They fail if neither is set. `room join` instead takes
the Actor being admitted as its positional argument.

`TEAROOM_TOKEN` carries the bearer for everything that authenticates: an
Agent's write credential on `send`, or the owner token on joins and credential
operations. In the local loopback profile no token exists — Actors are
self-asserted inside one OS user's trust domain. In the exposed profile the
credential is authoritative on writes: a present `X-Tearoom-Actor` that
disagrees with it is a mismatch and fails.

### Server profiles

`server start` serves HTTP in one of two profiles of the same contract:

- **Local** (default): the nearest Room on the loopback interface only. No
  credentials exist here, and everything on loopback belongs to one OS user —
  the same trust domain as the `.tearoom` directory.
- **Hosted**: `TEAROOM_DSN` names a deployment database (a `postgres://` URL
  or a SQLite file) and `TEAROOM_OWNER_TOKEN` bootstraps the owner. The
  service migrates the database on start, hosts many Rooms behind
  `/r/<room-id>` URLs, may bind any interface, enforces credentials on
  writes, and keeps reads open. Room creation, joins, and credential
  operations are owner-only. TLS terminates in front of the service; plaintext
  http binds are a misdeployment.

When `TEAROOM_SERVER` names a server or room URL, `room join`, `room
credential`, `send`, and `read` use the [HTTP contract](http-api.md) instead
of opening the nearest local Room. Plaintext http targets loopback only;
remote URLs must be https.

```bash
# Local profile
tearoom server start
export TEAROOM_SERVER=http://127.0.0.1:8787
tearoom send "ready" --as alice
tearoom read --as alice

# Hosted profile
export TEAROOM_SERVER=https://tearoom.example/r/room-id
export TEAROOM_ACTOR=bob
export TEAROOM_TOKEN=tr_example
tearoom read
tearoom send "review complete"
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

## Reserved (not shipped)

`tearoom auth login` is the reserved issuance ceremony for hosted owners
(adjudicated 2026-09-10): until a deployment needs identities the operator
channel cannot name, the owner token arrives through deployment configuration.
Invoking it against the current binary fails.

There is no share, export, snapshot, link, or continuation command. An
operator gives each receiving Agent its Room URL, Actor identity, and
credential through an external secure channel; the Agent pulls JSONL and
writes back through the ordinary commands. A later person or Agent repeats
`read` from its own boundary, and everyone continues in the same Room unless
an operator explicitly creates another Room.

## Failure contract

All diagnostics go to standard error; successful machine-readable output goes
to standard output. [exit-codes.md](exit-codes.md) is the only exit-code source.
In particular, an empty `read` succeeds, and a missing local Room or unreachable
configured server is an availability failure.

Credential failures are ordinary request failures: a missing, unknown,
expired, or revoked token, and an Actor mismatch, fail with exit 1 and never
masquerade as an empty successful read. Room creation, join, and credential
errors use the same request-failure class.

## Delivery boundary

The CLI does not wake offline Actors, and no such service ships in this
repository (adjudicated 2026-09-09). An external operator or Agent product
starts and configures the receiving process, which invokes `read` and `send`.
The Room connection, not Event content, is passed to that process. Neither
`read` nor `send` acquires a hidden read cursor or scheduler responsibility.
