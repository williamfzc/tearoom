---
type: reference
title: Storage layout
description: Target on-disk Room representation and the boundary around operational state.
resource: internal/store/file.go
tags: [reference, storage, concurrency]
timestamp: 2026-08-30
---

# Storage layout

## Target Room state

Every local Room is a `.tearoom` directory containing a backend marker. Both storage implementations sit behind the same Room contract:

```text
.tearoom/
  store.json      backend marker: {"backend":"file"|"sqlite"}
```

The file backend stores:

```text
.tearoom/
  store.json      backend marker
  room.json       Room identity and creation time
  actors.json     admitted Actor identities
  events.jsonl    ordered append-only Events
  lock            process coordination target
```

The SQLite backend stores the equivalent records in:

```text
.tearoom/
  store.json      backend marker
  room.db         Rooms, Actors, Events, and schema versions
```

The Event log is the durable collaboration truth. It contains no per-Actor read cursor, unread acknowledgement, activation delivery state, or Runtime session state. Those are caller-owned or application/control-plane state.

## Target append invariant

An append validates the Actor's admission, obtains the next dense Room sequence under the backend's atomic write boundary, and writes one immutable Event. Events are never rewritten through the Store API. No ordinary append needs to prove which Event a caller last read. Conditional append and idempotency remain open until a real protocol requires them.

## Operational state boundary

An optional activation service may keep its own ledger for pending, claimed, delivered, or failed delivery attempts. That ledger must be recoverable and idempotent, but it is not part of `.tearoom` Room history unless a later product decision gives it an explicit owner and format.

## Current implementation note

The shipped file Store still uses an exclusive `flock`, scans the current Event head, compares a caller-supplied expected cursor, checks idempotency keys, and persists client session cursors outside `.tearoom`. SQLite uses WAL and a busy timeout. These behaviors are current compatibility implementation details and conflict with the target append and observation model above.

The two backends remain separate creation choices: opening a Room dispatches on its marker, and there is no file-to-SQLite importer yet. PostgreSQL targets hosted operation with the same Store boundary; see [../architecture/persistence.md](../architecture/persistence.md).

## Portability

Unix-like systems use `flock` for the current file implementation. Windows has no safe cross-process implementation yet and must not be used concurrently.
