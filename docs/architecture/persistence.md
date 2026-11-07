---
type: architecture
title: Persistence direction
description: How one workspace stores the room tree and per-room record streams as JSONL files that travel with git.
tags: [architecture, persistence, jsonl, git, migration]
timestamp: 2026-11-07
---

# Persistence direction

The contract is `core.Store`: a room tree where each Room owns one ordered
immutable record stream. The storage is plain files — one JSONL stream per
Room — chosen so the workspace can live inside git: appends are new lines,
new Rooms are new directories, and the memory follows the checkout.
Implementation status belongs in [status.md](status.md); this document
defines the persistence shape and the migration discipline.

## The shape

One workspace database-free layout under `.tearoom`:

- `rooms/<name>/room.json` — identity: name, parent (omitted for a root),
  creation time. Each Room records only its own parent.
- `rooms/<name>/records.jsonl` — the stream: one JSON record per line, dense
  per Room, append-only.
- `rooms/<name>/idempotency.jsonl` — the per-(author, key) retry ledger.
- `store.json` — the format marker; `lock` — the flock target.

The child list of a Room is derived by reading the `rooms/` listing at read
time and is never cached or denormalized. There are no actor, membership, or
credential files: identity checks do not exist in the kernel.

## Ownership

- `internal/core` owns Room, Record, Notification, `Store`, and invariant
  language.
- `internal/store` owns the `.tearoom` layout, the format gate, the JSONL
  Store implementation, the cross-process lock, and the one-time migrations
  from the retired SQLite layouts.
- CLI and HTTP depend on `core.Store`, never on the file layout.

No SQL backend ships. The SQLite eras (formats 2 and 3) exist only as
migration sources; the pure-Go SQLite driver in the release binary exists
only to read them once.

## Append and read contract

An append holds the workspace lock (flock, exclusive) across room
verification, idempotency resolution, sequence assignment, and the append,
so concurrent processes receive one dense sequence per Room with no loss or
duplication. It validates nothing about identity — the signature is stored
as given — and appends one immutable line. Reading takes the shared lock and
is a repeatable query from a caller-supplied exclusive boundary; it never
advances stored state.

Idempotent append is matched per (room, author, key): an identical retry
(compact content, same signature) returns the original record; the same key
with different content conflicts. A signature may be empty.

## Migration discipline

Schema changes are versioned by the `store.json` format marker. The current
format is 4. Formats 2 and 3 (the SQLite eras) upgrade once, on open, into
the JSONL layout; the database file is deleted and the marker rewritten.
There is no import, no dual decoder, and no old/new double path: after the
one-time upgrade the old layout is never read again, and workspaces without
the marker (the v0 file layout or anything unrecognized) are refused, never
read, migrated, or reused. Rollback restores the previous release with its
data snapshot.

## Git and clones

Files make the workspace an ordinary git citizen. The boundary that follows
is documented in [the storage layout](../reference/storage-layout.md):
sequence guarantees hold within one checkout; merged clones reconcile like
any other merged text.

## Related

- [layers.md](layers.md)
- [status.md](status.md)
- [../reference/storage-layout.md](../reference/storage-layout.md)
