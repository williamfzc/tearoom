---
type: reference
title: CLI reference
description: Authoritative command contract for the tearoom binary over the room tree.
resource: internal/cli/cli.go
tags: [reference, cli, contract]
timestamp: 2026-11-07
---

# CLI reference

The CLI is Tearoom's top-level product interface. This page is its only command
contract. Implementation gaps belong in [status.md](../architecture/status.md),
and complete user journeys belong in [evaluations.md](../evaluations.md).

## Command contract

```text
tearoom room init --name <name> [--parent <room>]
tearoom send [--room <room>] --text <text> | --json <json> [--as <signature>] [--idempotency-key <key>]
tearoom read [--room <room>] [--after e<seq>] [--tail <n>] [--subtree]
tearoom tree
tearoom watch [--room <room>] [--json]
tearoom server start [--address 127.0.0.1:8787]
tearoom skill
tearoom --version
```

### Room lifecycle

- `room init --name <name>` creates a Room. Without `--parent` the Room is a
  root; with `--parent <room>` it records that existing Room as its parent.
  Room names are unique within the workspace.
- The first `room init` in a directory creates the workspace (`.tearoom`);
  later inits add Rooms to the same tree. There is no join, no admission, and
  no credential step.
- Success prints one JSON object:

```json
{"name":"implementation","parent":"main-line","created":"2026-11-07T10:00:00Z"}
```

Identifiers and timestamps above are examples, not fixed values.

### Append and read

- `send` appends one Record to the target Room and prints it as one JSON
  object. Plain text becomes a JSON string; `--json` accepts one valid JSON
  value. `--as` sets the signature — an unverified label; omitting it stores
  an empty signature.
- `--idempotency-key` is optional and scoped per (room, signature, key).
  Repeating the same key with the same content returns the original Record;
  reusing it with different content fails.
- `read` prints JSONL, one Record per line, no envelope, no saved cursor. An
  empty result prints nothing and exits 0.
  - `--after e42` starts after that exclusive boundary.
  - `--tail 5` prints only the last 5 records of each selected Room.
  - `--subtree` selects the Room and all its descendants (depth-first by
    name); each Record line carries its `room` field.
- A Record has this output shape:

```json
{"room":"implementation","seq":43,"author":"codex","at":"2026-11-07T10:01:00Z","content":{"plan":"review"}}
```

### Room selection

`send`, `read`, and `watch` target Rooms by name:

- With `--room <name>` the named Room is used; an unknown name is an error.
- Without `--room`, a workspace containing exactly one Room uses it; a
  workspace with several Rooms refuses and names the rule. `watch` without
  `--room` watches every Room in the workspace instead of refusing.

### Skeleton

`tree` prints the workspace skeleton: every root with its descendants,
depth-first by name. It derives the shape at read time and caches nothing.

```text
main-line
├── implementation
│   └── login-page
└── review
```

### Notifications

`watch` follows appends and prints one line per new Record. `--json` prints
the notification envelope, one JSON object per line:

```json
{"room":"implementation","seq":7,"author":"codex","at":"2026-11-07T10:02:00Z"}
```

The envelope carries no content and requests nothing; an external waker
decides what to start. `watch` runs until interrupted.

### Server

`server start` serves the nearest workspace over HTTP on the loopback
interface only — it has no credentials, and everything on loopback belongs to
one OS user, the same trust domain as the `.tearoom` directory. The HTTP
contract is [http-api.md](http-api.md). When `TEAROOM_SERVER` names a server
URL, `send`, `read`, and `watch` use it instead of the nearest local
workspace; room selection still follows the rules above.

```bash
# Local profile
tearoom server start
export TEAROOM_SERVER=http://127.0.0.1:8787
tearoom send --text "ready" --as codex
tearoom read --tail 5
```

### Agent guide

`tearoom skill` prints the orientation guide carried by the binary: the room
journey, the environment conventions, and the rules that hold at every
command. It takes no flags and reads no Room.

## Signature selection

`send` and `read` take `--as` for a one-off signature, falling back to
`TEAROOM_ACTOR`. A missing signature is not an error: the record simply
carries an empty author. Nothing validates the value.

## Failure contract

All diagnostics go to standard error; successful machine-readable output goes
to standard output. [exit-codes.md](exit-codes.md) is the only exit-code
source. In particular, an empty `read` succeeds, and a missing workspace or
unreachable configured server is an availability failure (exit 4), while an
unknown room name or an idempotency conflict is a request failure (exit 1).

## Delivery boundary

The CLI does not wake anyone. `watch` emits notifications; an external waker
reads them and starts the next worker. No scheduler, launcher, or attention
management ships in this repository.
