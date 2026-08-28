---
type: architecture
title: Layers and dependency direction
description: Package responsibilities and the rule that shared invariants live below every client.
resource: internal/
tags: [architecture, boundaries, layering]
timestamp: 2026-08-28
---

# Layers and dependency direction

| Layer | Package | Owns |
|---|---|---|
| Foundation | `internal/core` | Room, Actor, Event, Store, typed errors |
| Domain | `internal/store` | File layout, cross-process locking, append invariants |
| Outside | `internal/transport` | HTTP encoding and remote Store implementation |
| Outside | `internal/cli`, `cmd/tearoom` | Command routing, local session cursor, text/JSON output, process entry point |

Dependencies point inward. `core` imports no project package. `store` and `transport` implement `core.Store`. `cli` depends on the interface and chooses a local or remote implementation.

## Store boundary

These rules are enforced inside every Store implementation, not remembered by callers:

- joined Actor requirement;
- recipient existence;
- room cursor freshness;
- idempotency key uniqueness;
- dense sequence assignment;
- append-only persistence;
- visibility filtering.

The file Store checks and appends under one `flock`. The HTTP server delegates directly to Store, so a raw HTTP client cannot bypass rules that the CLI obeys.

## Why there is no projection layer yet

The previous model needed dedicated assembly and view packages because it stored several related entities and derived a board from them. The Room model has no built-in board. Reads return Events and a cursor directly, so those packages had no remaining responsibility and were removed rather than retained as empty architecture.

Application-specific projections should live outside the kernel until they have multiple callers and a stable owner.

## Related

- [../domain/concepts.md](../domain/concepts.md)
- [../lessons/invariants-belong-in-the-store.md](../lessons/invariants-belong-in-the-store.md)
