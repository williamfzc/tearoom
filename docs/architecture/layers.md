---
type: architecture
title: Layers and dependency direction
description: The package boundaries of the room-tree kernel and its herdr-core collaboration integration.
resource: internal/
tags: [architecture, boundaries, layering, clean-break]
timestamp: 2026-11-07
---

# Layers and dependency direction

The kernel has one foundation contract and no compatibility layer:

| Layer | Package or owner | Owns |
|---|---|---|
| Foundation | `internal/core` | Room, Record, Notification, the `Store` seam, and typed domain errors |
| Persistence | `internal/store` | The `.tearoom` JSONL layout, the JSONL Store implementation, the cross-process lock, and the one-time migrations from the retired SQLite formats |
| Outside | `internal/transport` | The minimal local HTTP face of the `Store` contract |
| Outside | `internal/cli`, `cmd/tearoom` | `room`, `send`, `read`, `tree`, `watch`, `server`, output, and process entry |

Dependencies point inward. `core` imports no project package. Persistence
implements `core.Store`; transport adapts it; the CLI translates commands. The
kernel never imports a scheduler, launcher, or Agent runtime. Credentials,
admission, and the hosted multi-room service were removed with the v1 model on
2026-11-07 (see [../log.md](../log.md)).

## Foundation boundary

`internal/core` defines only the shared collaboration truth:

- Room identity: a name, an optional parent, and a creation time;
- Record: `seq`, `author`, `at`, `content` — the author is an unverified
  label, and message addressing remains in content/convention rather than a
  recipient field;
- Notification: room, sequence, signature, time — derived, never stored;
- the `Store` seam: create a room (optionally under a parent), list rooms,
  read a room's stream from a boundary, append one record;
- typed errors that describe that contract.

The Store never checks identity: reading and writing require no admission,
and no Store operation creates membership as a side effect. What one Room
sees is bounded by the tree, not by permissions.

## Persistence boundary

`internal/store` owns the `.tearoom` layout: one workspace directory holds
the whole room tree as JSONL files behind a format marker, and the JSONL
Store implements `core.Store` under a cross-process flock. It may not:

- filter records by author or message addressing;
- validate or transform a signature beyond storing it verbatim;
- cache a parent's child list or any derived tree shape;
- host delivery claims, wake state, or process state.

One append holds the exclusive lock across room verification, idempotency
resolution, sequence assignment, and the append, so concurrent writers
receive a dense per-room sequence. The one-time format-2/3 → format-4
migrations are specified in [persistence.md](persistence.md).

## Outside boundaries

`internal/transport` exposes the minimal local HTTP face: room list, record
read, record append. It holds no authentication paths — the exposed profile,
owner tokens, and credential routes died with the hosted service.

`internal/cli` exposes the currently shipped command surface:

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

## Collaboration runtime boundary

`herdr-core` (`hc`) is the intended runtime foundation for Agent terminals,
prompt delivery, parent-child execution, and lifecycle events. It has no
native Tearoom board or role-routing policy. An integration outside the
kernel defines reusable roles (command, working directory, and role
instructions), configures collaborators when creating a Room, and binds each
Room's role instance to its own hc panel. All those roles share the Room's one
record stream; reusing a role definition in another Room creates a separate
panel instance. `@role` routes through hc to that Room-scoped panel. Different
Rooms coordinate through a shared ancestor Room; build and review use separate
Agent instances and roles in the same Room.

The integration also exposes the configured collaborators to the CLI and
reconciles Room records with hc snapshots/events after interruption. The
collaborator roster is discovery and routing data, not admission; it does not
affect who can read or write a local Room. hc template text is an initial
instruction, not a native system prompt. Panel-failure handling and reliable
recovery are not existing hc guarantees. The delivery contract is in
[collaboration.md](collaboration.md); configuration is in the
[Agent integration guide](../reference/agent-integration.md).
The integration may depend on `core` and `transport`; neither may depend on it.

## Clean-break dependency checks

1. `core` has no import path to persistence, transport, or CLI.
2. Every Store implementation satisfies one interface; there is no parallel
   old/new reader or writer.
3. No Store operation validates identity or admits anyone.
4. Transport has one route/body/error vocabulary with no authorization paths.
5. CLI, scripts, tests, and docs use only the command and record vocabulary
   above.
6. The hc integration stays outside the kernel and does not add
   recipient, membership, or process-state fields to core Records.

`make check`, `make docs`, and repository searches verify these boundaries.
Rollback is a Git rollback to a matched revision and data snapshot; adding a
deprecated dependency or compatibility layer is not an allowed recovery.

## Related

- [persistence.md](persistence.md)
- [status.md](status.md)
- [../domain/concepts.md](../domain/concepts.md)
