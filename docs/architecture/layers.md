---
type: architecture
title: Layers and dependency direction
description: The package boundaries of the room-tree kernel — core, persistence, transport, and CLI.
resource: internal/
tags: [architecture, boundaries, layering, clean-break]
timestamp: 2026-11-07
---

# Layers and dependency direction

The kernel has one foundation contract and no compatibility layer:

| Layer | Package or owner | Owns |
|---|---|---|
| Foundation | `internal/core` | Room, Record, Notification, the `Store` seam, and typed domain errors |
| Persistence | `internal/store`, `internal/relational` | The `.tearoom` workspace layout and the durable room tree with per-room record streams |
| Outside | `internal/transport` | The minimal local HTTP face of the `Store` contract |
| Outside | `internal/cli`, `cmd/tearoom` | `room`, `send`, `read`, `tree`, `watch`, `server`, output, and process entry |

Dependencies point inward. `core` imports no project package. Persistence
implements `core.Store`; transport adapts it; the CLI translates commands. The
kernel never imports a scheduler, launcher, or Agent runtime, and there is no
control-plane package: credentials, admission, and the hosted multi-room
service were removed with the v1 model on 2026-11-07 (see
[../log.md](../log.md)).

## Foundation boundary

`internal/core` defines only the shared collaboration truth:

- Room identity: a name, an optional parent, and a creation time;
- Record: `seq`, `author`, `at`, `content` — the author is an unverified
  label, and there is no recipient field;
- Notification: room, sequence, signature, time — derived, never stored;
- the `Store` seam: create a room (optionally under a parent), list rooms,
  read a room's stream from a boundary, append one record;
- typed errors that describe that contract.

The Store never checks identity: reading and writing require no admission,
and no Store operation creates membership as a side effect. What one Room
sees is bounded by the tree, not by permissions.

## Persistence boundary

`internal/store` owns the `.tearoom` layout: one workspace directory holds
the whole room tree in one SQLite database behind a format marker.
`internal/relational` owns ORM models, migrations, transactions, and database
adaptation (SQLite locally; the same schema targets PostgreSQL for tests and
future serving). Neither package may:

- filter records by author or recipients (there are no recipients);
- validate or transform a signature beyond storing it verbatim;
- cache a parent's child list or any derived tree shape;
- host delivery claims, wake state, or process state.

One append transaction serializes on the target Room's row, so concurrent
writers receive a dense per-room sequence while different Rooms never wait on
each other. The one-time format-2 → format-3 migration is specified in
[persistence.md](persistence.md).

## Outside boundaries

`internal/transport` exposes the minimal local HTTP face: room list, record
read, record append. It holds no authentication paths — the exposed profile,
owner tokens, and credential routes died with the hosted service.

`internal/cli` exposes only the command surface:

```text
tearoom room init --name <name> [--parent <room>]
tearoom send [--room <room>] --text <text> | --json <json> [--as <signature>]
tearoom read [--room <room>] [--after e<seq>] [--tail <n>] [--subtree]
tearoom tree
tearoom watch [--room <room>] [--json]
tearoom server start
tearoom skill
```

It owns formatting and invocation-local options, not shared invariants. The
v1 commands `room join`, `room credential`, and the hosted `server` profile
are deleted; no alias keeps them alive.

## Clean-break dependency checks

1. `core` has no import path to persistence, transport, or CLI.
2. Every Store implementation satisfies one interface; there is no parallel
   old/new reader or writer.
3. No Store operation validates identity or admits anyone.
4. Transport has one route/body/error vocabulary with no authorization paths.
5. CLI, scripts, tests, and docs use only the command and record vocabulary
   above.

`make check`, `make docs`, and repository searches verify these boundaries.
Rollback is a Git rollback to a matched revision and data snapshot; adding a
deprecated dependency or compatibility layer is not an allowed recovery.

## Related

- [persistence.md](persistence.md)
- [status.md](status.md)
- [../domain/concepts.md](../domain/concepts.md)
