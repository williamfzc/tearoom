---
type: reference
title: Storage layout
description: Authoritative on-disk Room representation and the boundary around operational state.
resource: internal/store/open.go
tags: [reference, storage, sqlite]
timestamp: 2026-09-10
---

# Storage layout

## Room state

Every local Room is a `.tearoom` directory containing a format marker and one
SQLite database behind the single Room contract:

```text
.tearoom/
  store.json      format marker: {"format":2,"backend":"sqlite"}
  room.db         Rooms, Actors, Events, and idempotency records
```

The marker is the format gate: only `format: 2` with backend `sqlite` is
accepted. A markerless, version-1, or retired-backend layout is refused with
an incompatible-store error; there is no import and no automatic upgrade. The
database runs in WAL mode with a busy timeout, so concurrent processes append
safely without any external lock file.

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

No in-product delivery or wake service exists (adjudicated 2026-09-09; see
[product.md](../product.md)); this Room layout does not
store Actor read progress, delivery attempts, bindings, or Runtime state.

There is one local backend and no creation choice to make. The clean break
does not import pre-adoption Room data or session state: an old `.tearoom`
directory is an incompatible store. PostgreSQL is the remote database adapter
for the same service, schema owner, and Store boundary; see
[persistence.md](../architecture/persistence.md).

## Portability

The storage engine is the pure-Go `glebarez/sqlite` driver, so release
binaries stay static (`CGO_ENABLED=0`) and SQLite's own locking serializes
concurrent writers on every platform.
