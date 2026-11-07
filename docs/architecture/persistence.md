---
type: architecture
title: Persistence direction
description: How one workspace stores the room tree, per-room record streams, and the one-time format migration.
tags: [architecture, persistence, sqlite, postgresql, migration]
timestamp: 2026-11-07
---

# Persistence direction

The contract is `core.Store`: a room tree where each Room owns one ordered
immutable record stream. Database records and ORM APIs stay behind that
boundary. SQLite is shipped; the same schema and transactions target
PostgreSQL for the gated battery and future multi-host serving. Implementation
status belongs in [status.md](status.md); this document defines only the
persistence shape and the migration boundary.

## Deployment matrix

| Deployment profile | Storage | Purpose |
|---|---|---|
| Local | SQLite, one `.tearoom` workspace | One-machine setup; the only shipped profile |
| Gated / future | PostgreSQL, the same schema | Concurrent instances, backups, and future serving |

One relational schema owner and migration sequence serves both. Backend
selection is never a user-facing flag.

## Ownership

- `internal/core` owns Room, Record, Notification, `Store`, and invariant
  language.
- `internal/relational` owns ORM models, migrations, queries, and
  transactions.
- `internal/store` owns the `.tearoom` layout and the format marker.
- CLI and HTTP depend on `core.Store`, never on ORM or concrete database
  types.

The ORM is replaceable infrastructure; its model types are storage rows, not
domain types. The shipped backend uses GORM with the pure-Go
`glebarez/sqlite` dialector, keeping release binaries static
(`CGO_ENABLED=0`).

## Relational shape

One workspace database holds the whole tree:

- `rooms` — name (primary key), parent (nullable: null is a root), creation
  time, and the dense sequence head;
- `events` — room, sequence, author (a verbatim label), acceptance time, and
  content as compact JSON text, keyed by (room, seq);
- `append_idempotency` — the per-(room, signature, key) ledger recording the
  fingerprint of the original request and the sequence it created; it is
  never exposed as a record field;
- `meta` — the format marker.

Content is stored and transported as compact JSON; plain text is a JSON
string. The child list of a Room is derived by querying `rooms` at read time
and is never cached or denormalized. There are no actor, membership, or
credential tables: identity checks do not exist in the kernel.

## Append and read contract

An append is serialized on the target Room's row and assigns the next dense
sequence of that Room. It validates nothing about identity — the signature is
stored as given — and inserts the immutable record and the new head
all-or-none. Reading is a repeatable query from a caller-supplied exclusive
boundary; it never advances stored state.

Idempotent append is matched per (room, signature, key): an identical retry
(compact content, same signature) returns the original record; the same key
with different content conflicts. A signature may be empty; the fingerprint
still distinguishes keys per signature.

## Migration discipline

Schema changes are versioned by the `meta` format marker. The current format
is 3. Format 2 workspaces (the v1 single-room model) upgrade once, on open:

- the single room row becomes a root Room named by its old identifier;
- every record keeps its author verbatim; the recipient column is dropped;
- the actors and credentials tables are dropped.

There is no import, no dual decoder, and no old/new double path: after the
one-time upgrade the old layout is never read again, and a database without
the marker is refused, never read or upgraded. Rollback restores the previous
release with its data snapshot.

## Related

- [layers.md](layers.md)
- [status.md](status.md)
- [../reference/storage-layout.md](../reference/storage-layout.md)
