---
type: reference
title: Storage layout
description: The on-disk Room representation and locking discipline.
resource: internal/store/file.go
tags: [reference, storage, concurrency]
timestamp: 2026-08-28
---

# Storage layout

```text
.tearoom/
  room.json       Room identity and creation time
  actors.json     joined Actor identities
  events.jsonl    ordered append-only Events
  lock            flock target for reads and mutations
```

Client session cursors are deliberately not stored in `.tearoom`. They live in the user's Tearoom configuration directory with owner-only permissions and are scoped by Room and Actor, so one shared Room does not mix private client progress into its durable state.

This document describes the current file Store, not the only permitted backend. The planned relational Store uses SQLite locally and PostgreSQL for hosted deployments while preserving the same `core.Store` contract. See [../architecture/persistence.md](../architecture/persistence.md).

## Atomic append

The file Store holds an exclusive `flock` while it:

1. verifies the Actor and recipients;
2. scans the current Event head and idempotency keys;
3. compares the expected cursor;
4. assigns the next dense sequence;
5. appends and syncs one JSON line.

Holding the lock over the entire decision prevents two independent processes from both accepting the same cursor.

Actor updates are written through a temporary file and renamed. Events are never rewritten through the Store API.

## Portability

Unix-like systems use `flock`. Windows has no safe cross-process implementation yet and must not be used concurrently.
