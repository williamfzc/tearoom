---
type: reference
title: Storage layout
description: Authoritative on-disk workspace representation and the one-time format migration.
resource: internal/store/open.go
tags: [reference, storage, sqlite, migration]
timestamp: 2026-11-07
---

# Storage layout

## Workspace state

Every workspace is a `.tearoom` directory containing a format marker and one
SQLite database holding the whole room tree:

```text
.tearoom/
  store.json      format marker: {"format":3,"backend":"sqlite"}
  room.db         Rooms (with parents), Records, idempotency records
```

The marker is the format gate: `format: 3` with backend `sqlite` is the
current layout, and `format: 2` is the v1 single-room layout, upgraded once
on open (below). Any other layout — markerless, version 1, or a retired
backend — is refused with an incompatible-store error; there is no import and
no automatic upgrade beyond that one step. The database runs in WAL mode with
a busy timeout, so concurrent processes append safely without any external
lock file.

## Tables

- `rooms` — `name` (primary key), `parent` (nullable; null is a root),
  `created_at`, and the dense sequence `head`.
- `events` — `room_id`, `seq` (primary key pair), `author` (verbatim label),
  `at`, `content` (compact JSON text).
- `append_idempotency` — one row per (room, signature, key): the fingerprint
  of the original request and the sequence it created. The key is never
  exposed as a Record field.
- `meta` — the format marker row.

The record log is the durable collaboration truth. It contains no read
cursor, no unread acknowledgement, no delivery state, and no runtime session
state; those are caller-owned or external. A parent's child list is derived
by querying `rooms` and is never cached.

## The one-time format-2 → format-3 migration

A format-2 workspace stores the v1 model: one Room per workspace, admitted
actors, and credentials. Opening it upgrades once:

- the single room row becomes a root Room named by its old identifier (its
  parent is null — a parentless room is a root);
- every record keeps its author verbatim; the recipient column is dropped;
- the `actors` and `credentials` tables are dropped;
- the marker is rewritten to format 3.

There is no compatibility layer: after the upgrade the old layout is never
read again, and databases without the marker are refused, never read,
migrated, or reused.

## Admission-free invariants

- Reading or sending never creates Rooms; only `room init` does.
- An append validates nothing about identity, obtains the next dense
  sequence of the target Room under the database's atomic write boundary, and
  writes one immutable Record. Records are never rewritten through the Store
  API.
- An append with an idempotency key is matched per (room, signature, key): an
  equal fingerprint returns the original Record; a different one conflicts.

## Portability

The storage engine is the pure-Go `glebarez/sqlite` driver, so release
binaries stay static (`CGO_ENABLED=0`) and SQLite's own locking serializes
concurrent writers on every platform. PostgreSQL runs the same schema through
the same migrations for the gated battery and future serving; see
[persistence.md](../architecture/persistence.md).
