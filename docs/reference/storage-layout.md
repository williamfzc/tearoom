---
type: reference
title: Storage layout
description: Authoritative on-disk workspace representation — JSONL files that travel with git — and the format gate.
resource: rust/src/store.rs
tags: [reference, storage, jsonl, git, migration]
timestamp: 2026-11-07
---

# Storage layout

## Workspace state

Every workspace is a `.tearoom` directory. Room history uses plain files: one
directory and one JSONL stream per Room, plus a format marker. Collaboration
and Herdr integration also keep configuration and runtime state there. The
workspace's local or versioned status is a repository policy; see the
[repository overview](../../README.md) for this checkout.

```text
.tearoom/
  store.json                        format marker: {"format":4,"backend":"jsonl"}
  lock                              flock target for cross-process serialization
  collaboration.json               reusable Role definitions and Room rosters
  collaboration.lock               serializes collaboration configuration updates
  dispatch.json                    durable hc delivery intents and read cursors
  .dispatcher.lock                 admits one running dispatcher per workspace
  .dispatch.lock                   serializes delivery state changes
  herdr.json                       local Room-to-Herdr session and pane bindings
  herdr.lock                        serializes Herdr binding updates
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
current layout; formats 2 and 3 (the SQLite eras) are refused with an error
naming an earlier release — the one-time migration to format 4 is an
operator step with the final Go release (tag `go-final`); any other layout —
markerless (the v0 file layout), version 1, or a retired backend — is
refused with an incompatible-store error.

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
- `collaboration.json` stores reusable Role launch definitions and each Room's
  routing roster. The roster routes addressed messages; it does not grant
  access to Room history or writes.
- `dispatch.json` stores the delivery intents and per-Room read cursors used by
  the local hc dispatcher. `.dispatcher.lock` admits one running dispatcher;
  `.dispatch.lock` serializes its state changes with manual operations.
- `herdr.json` stores the local Herdr session, workspace and pane associations,
  including prior shared-session bindings when migrated. It is
  optional integration state; Room records remain usable if Herdr is absent.

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

## Legacy formats

Formats 2 and 3 (the SQLite eras) are not read by this binary. An operator
holding such a workspace runs the final Go release once (tag `go-final`),
which performs the one-time export — format 2 (the v1 single-room model)
becomes a root named by its old identifier with authors kept verbatim;
format 3 (the SQLite room tree) exports the tree and its records verbatim —
deletes the database, and rewrites the marker to format 4. After that step
the current binary reads the workspace natively. There is no compatibility
layer in either release, and markerless databases are refused, never read or
reused. Refusing on open keeps an automatic migration from racing an old
process still writing the database. Rollback restores the previous release
with its data snapshot.

## Git and clones

The layout is ordinary text: a workspace can be committed, diffed, and
merged like any other file in the repository. One boundary follows: dense
sequence numbers are guaranteed within one checkout — concurrent writers on
one machine serialize through the lock; two clones that both append and
merge will need a human or Agent to reconcile the streams, exactly as with
any other merged text.

## Portability

The storage engine is plain files with no database dependency, so release
binaries stay static on every platform. See
[persistence.md](../architecture/persistence.md) for the persistence
discipline behind this layout.
