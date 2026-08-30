---
type: reference
title: Storage layout
description: Authoritative on-disk Room representation and the boundary around operational state.
resource: internal/store/file.go
tags: [reference, storage, concurrency]
timestamp: 2026-08-30
---

# Storage layout

## Room state

Every local Room is a `.tearoom` directory containing a backend marker. Both
storage implementations sit behind the same Room contract:

```text
.tearoom/
  store.json      backend marker: format 2, {"backend":"file"|"sqlite"}
```

The marker is the format gate: only `format: 2` is accepted. A markerless,
version-1, or unknown-backend layout is refused with an incompatible-store
error; there is no import and no automatic upgrade.

The file backend stores:

```text
.tearoom/
  store.json      backend marker
  room.json       Room identity and creation time
  actors.json     admitted Actor identities
  events.jsonl    ordered append-only Events with idempotency metadata
  lock            flock coordination target, created on demand
```

`events.jsonl` keeps an internal record envelope: the Event plus the request
idempotency key and fingerprint used to answer retries. Only Events are ever
exposed through the Store contract.

The SQLite backend stores the equivalent records in:

```text
.tearoom/
  store.json      backend marker
  room.db         Rooms, Actors, Events, and idempotency records
```

The relational schema is versioned and refuses to open a database without the
format-2 marker. `append_idempotency` is a separate table: the idempotency key
is persisted per `(room, actor, key)` but never becomes an Event field.

The Event log is the durable collaboration truth. It contains no per-Actor
read cursor, unread acknowledgement, activation delivery state, or Runtime
session state. Those are caller-owned or application/control-plane state.

## Admission and append invariants

- Admission is explicit: an Actor exists only after `room init` (creator) or
  `room join`. Reading or sending never creates membership.
- An append validates the Actor's admission, obtains the next dense Room
  sequence under the backend's atomic write boundary, and writes one immutable
  Event. Events are never rewritten through the Store API.
- No ordinary append proves which Event a caller last read; there is no
  expected-cursor path. Conditional append remains open.
- An append with an idempotency key is matched per `(room, actor, key)`: an
  equal fingerprint (compacted content, normalized `to`, Actor) returns the
  original Event; a different one conflicts.

## Operational state boundary

An optional activation service keeps its operational ledger outside Room
history. Its owner, durability, and recovery contract are defined in
[activation.md](../architecture/activation.md); this Room layout does not
store Actor read progress, delivery attempts, bindings, or Runtime state.

The two backends are separate creation choices. The clean break does not
import pre-adoption Room data or session state. PostgreSQL targets a future
hosted operation with the same Store boundary; see
[persistence.md](../architecture/persistence.md).

## Portability

Unix-like systems use `flock` for the current file implementation. Windows has
no safe cross-process implementation yet and must not be used concurrently.
