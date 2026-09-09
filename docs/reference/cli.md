---
type: reference
title: CLI reference
description: Authoritative command contract for the shipped tearoom binary.
resource: internal/cli/cli.go
tags: [reference, cli, contract]
timestamp: 2026-08-30
---

# CLI reference

This is the only CLI contract. `read` is the normal observation command,
waiting is a read mode, and no command depends on a server-owned Actor cursor.
Implementation status belongs in [status.md](../architecture/status.md).

## Command contract

```text
tearoom room init [--as <actor>] [--store file|sqlite]
tearoom room join <actor>
tearoom room members

tearoom send <text> [--as <actor>] [--to <member>]... [--idempotency-key <key>]
tearoom send --json <json> [--as <actor>] [--to <member>]...

tearoom read [--as <actor>] [--after e<seq>] [--limit N] [--wait <duration>]

tearoom server start [--address 127.0.0.1:8787]
```

- `room init` atomically creates the Room and admits the creator as its first
  member. The Room identity comes from the directory that owns `.tearoom`;
  there is no `--name` flag. `--store` selects the backend: `file` (default)
  or `sqlite`.
- `room join <actor>` is explicit admission within the trust domain of one OS
  user. Joining an already-admitted Actor returns the existing Actor.
- `room members` lists the admitted Actors.
- `send` appends one Event and prints it as one JSON object. Plain text is
  encoded as a JSON string; `--json` passes the raw JSON content verbatim.
  `--to` names addressees and is repeatable. It is public addressing metadata,
  never a visibility boundary.
- `read` prints JSONL: one Event per line, no envelope and no cursor. An empty
  result prints nothing and exits 0. `--after` accepts only the presentation
  address `e<seq>` as the exclusive caller-owned boundary. `--limit` truncates
  the result. `--wait` holds only the current invocation until an Event past
  the boundary exists, and exits 3 when the bound elapses with nothing new.
- `server start` serves the nearest Room over HTTP. P0 refuses non-loopback
  listen addresses: the HTTP contract carries no authentication, so the
  service must not be exposed to a network.
- Actor identity comes from `TEAROOM_ACTOR` or `--as`; an identity is required
  for `init`, `join`, `send`, and `read`. There is no anonymous fallback.

## Server target mode

When `TEAROOM_SERVER` is set to a server URL, `join`, `members`, `send`, and
`read` speak the [HTTP contract](http-api.md) instead of opening a local Room.
`room init` is always local and refuses to run while `TEAROOM_SERVER` is set.
There is no token: the served Room is protected by the loopback trust domain.

## Exit codes

The machine-readable outcomes are in [exit-codes.md](exit-codes.md): 0 on
success (including an empty `read`), 1 on any error — an HTTP 409 idempotency
conflict is exit 1 — 3 for a `read --wait` timeout, and 4 when no Room or the
server is unreachable.

## Output and encoding

- JSON is the default output for `init`, `join`, `members`, and `send`.
- `read` emits JSONL Events. Events carry `seq`, `actor`, `at`, optional
  `to`, and `content`; `content` is always valid JSON.
- Plain-text sends are stored as JSON strings, so `read` output is always
  machine-parseable.

## Activation boundary

The CLI does not wake offline Actors, and no such service ships in this
repository (adjudicated 2026-09-09): delivery is a paste, a context link, or an
operator-owned external script that invokes `read` and `send`. Neither `read`
nor `send` acquires a hidden read cursor or scheduler responsibility.
