---
type: reference
title: Storage layout
description: Authoritative on-disk workspace representation — JSONL files that travel with git — and the one-time format migrations.
resource: internal/store/open.go
tags: [reference, storage, jsonl, git, migration]
timestamp: 2026-11-07
---

# Storage layout

## Workspace state

Every workspace is a `.tearoom` directory of plain files: one directory per
Room, one JSONL stream per Room, and a format marker. Nothing else exists —
no database, no binary state. The layout is chosen so the whole tree can be
committed to git: appends are new lines, new Rooms are new directories, and
history diffs cleanly.

```text
.tearoom/
  store.json                        format marker: {"format":4,"backend":"jsonl"}
  lock                              flock target for cross-process serialization
  rooms/
    main-line/
      room.json                     {"name":"main-line","created":"..."}
      records.jsonl                 one Record per line, seq 1..n
      idempotency.jsonl             one ledger line per (author, key) use
      append-pending.json            temporary durable intent during a keyed append
    implementation/
      room.json                     {"name":"implementation","parent":"main-line","created":"..."}
      records.jsonl
```

The marker is the format gate: `format: 4` with backend `jsonl` is the
current layout; formats 2 and 3 (the SQLite eras) upgrade once on open; any
other layout — markerless (the v0 file layout), version 1, or a retired
backend — is refused with an incompatible-store error.

## The files

- `room.json` carries the Room's identity: its name, its parent (omitted for
  a root — a parentless room is a root), and its creation time. Each Room
  records only its own parent; the child list of any Room is derived by
  reading the `rooms/` listing at query time and is never cached.
- `records.jsonl` is the Room's stream: one JSON object per line, in sequence
  order — `{"room":...,"seq":...,"author":...,"at":...,"content":...}` — the
  same shape the CLI prints. Records are appended, never rewritten.
- `idempotency.jsonl` is the Room's retry ledger: one line per
  (author, key) use — `{"author":...,"key":...,"fingerprint":...,"seq":...}`.
  An equal retry returns the original record; the same key with different
  content conflicts. The key is never exposed as a record field.
- `append-pending.json` is absent between operations. During a keyed append it
  durably records the intended Record and ledger entry before either JSONL
  append. The next writer replays that intent under the workspace lock, filling
  in whichever append is missing, then removes the intent. Existing workspaces
  need no conversion; the file appears only while a keyed append is in flight
  or awaiting recovery.

The directory is the Room's address; the name inside `room.json` and the
`room` field on each record line exist so a moved or edited file fails
closed instead of silently re-identifying. Room names are directory names:
no `/`, no leading dot, and never `.` or `..`.

## Serialization and durability

One `flock` on `.tearoom/lock` serializes writers across processes: an
append holds the exclusive lock across recovery, room verification,
idempotency resolution, sequence assignment, and all writes. Keyed appends
write and sync the pending intent before appending the Record and ledger entry;
the intent is removed only after both JSONL files are synced. An interrupted
append is completed by the next append. Recovery may truncate an unparseable
final fragment only when those bytes are a prefix of the exact Record or ledger
line in the pending intent; unrelated bytes and complete but mismatching
unterminated entries fail closed. Truncation preserves all preceding newline
terminated lines. No whole-file rewrites or migrations are performed. Reads
remain side-effect-free; if a crash left an incomplete tail, a read reports
corruption until a retrying append performs recovery.
Reads take the shared lock and are pure: no cursor is stored, and repeated
reads return identical history. The Windows gap stands: concurrent use is
unsupported where no cross-process lock exists.

## The one-time migrations

- **Format 2 → 4** (the v1 single-room SQLite model): the room becomes a
  root named by its old identifier, records keep their author verbatim, the
  recipient column and the actor and credential tables are dropped.
- **Format 3 → 4** (the SQLite room tree): the tree and its records export
  verbatim.

Both run once, on open: the SQLite database is read, the JSONL layout is
written, the database file is deleted, and the marker is rewritten. There is
no compatibility layer after it, and databases without the marker are
refused, never read or reused. Rollback restores the previous release with
its data snapshot.

## Git and clones

The layout is ordinary text: a workspace can be committed, diffed, and
merged like any other file in the repository. One boundary follows: dense
sequence numbers are guaranteed within one checkout — concurrent writers on
one machine serialize through the lock; two clones that both append and
merge will need a human or Agent to reconcile the streams, exactly as with
any other merged text.

## Portability

The storage engine is plain files, so release binaries stay static
(`CGO_ENABLED=0`) on every platform. The legacy SQLite reader used only by
the one-time migrations is pure Go. See
[persistence.md](../architecture/persistence.md) for the persistence
discipline behind this layout.
