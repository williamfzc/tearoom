---
type: architecture
title: Layers and dependency direction
description: Package responsibilities and the boundary between the Room kernel, clients, and optional Agent activation.
resource: internal/
tags: [architecture, boundaries, layering]
timestamp: 2026-08-30
---

# Layers and dependency direction

The target model has one durable coordination kernel and optional application
capabilities above it:

| Layer | Current package or owner | Target responsibility |
|---|---|---|
| Foundation | `internal/core` | Room, Actor, Event, Store, and typed errors |
| Persistence | `internal/store`, relational Store | Durable Room membership and ordered Event history |
| Application | not yet implemented | Optional activation ledger, policy, runtime binding, and launcher |
| Outside | `internal/transport` | HTTP encoding and remote Store implementation |
| Outside | `internal/cli`, `cmd/tearoom` | Command routing, session state during migration, output, and process entry |

Dependencies point inward. `core` imports no project package. Persistence and
transport implement `core.Store`; the CLI depends on that interface. A future
activation service may consume and append through `core.Store`, but the kernel
must not import a launcher, scheduler, Agent runtime, or hosted control plane.

## Target Store boundary

The Store owns the facts that every client must share:

- Room identity and membership admission;
- Actor identity and authorization checks supplied by the enclosing boundary;
- recipient validity for public `to` addressing;
- dense sequence assignment and append-only persistence;
- immutable Event reads in Room order;
- any explicitly adopted idempotency contract.

Membership determines visibility: every admitted Room member reads the complete
Room history. `to` identifies members who should pay attention; it is not a
secrecy filter and does not request process execution. Read progress is not
hidden Store state: a pure read takes an explicit boundary such as `after seq`,
or a separately named consumer identity may own durable delivery progress.

The current implementation does not yet satisfy this target boundary. Its
Store still enforces a saved per-Actor cursor for writes and filters directed
Events, while the CLI auto-registers Actors. Those are compatibility facts to
remove through an explicit migration, not target invariants.

## Activation boundary

Optional offline activation belongs in the application layer. Its durable state
may record `(room, event sequence, target actor, activation)` with claim, lease,
attempt, retry, and failure information. It must not be represented as Actor
read progress or as an Event completion claim.

The application layer owns policy and runtime bindings:

```text
addressed Event -> activation policy -> durable claim/lease
                -> operator-owned launcher -> ephemeral Runtime
                -> Runtime reads Room and appends a contribution
```

The Room Event remains the source of truth. Activation is at-least-once; a
launcher and Runtime must make duplicate attempts safe. Without an activation
service, sending an Event stops after durable append.

## No projection layer yet

The Room model has no built-in board, workflow, task, lease, or status view.
Application-specific projections belong above the kernel until they have
multiple callers and a stable owner. This keeps protocol conventions and
replay views from becoming hidden core entities.

## Migration note

The existing code, tests, and HTTP contract still describe the older model:
`event list`/`watch`, local saved cursors, directed visibility, and mandatory
freshness on append. Any layer change must update those boundaries together and
must not claim that optional activation exists before a real offline-runtime
journey has been proven.

## Related

- [model-discussion.md](model-discussion.md)
- [persistence.md](persistence.md)
- [../domain/concepts.md](../domain/concepts.md)
- [../lessons/invariants-belong-in-the-store.md](../lessons/invariants-belong-in-the-store.md)
