---
type: architecture
title: Implementation status
description: What the code ships today against the room-tree model and what remains open — the single implementation-gap source.
tags: [architecture, status]
timestamp: 2026-11-07
---

# Implementation status

This page records what the shipped code implements against
[../product.md](../product.md). History and rationale live in
[../log.md](../log.md); package ownership and dependency direction in
[layers.md](layers.md); the storage boundary in [persistence.md](persistence.md);
executable proof in [../evaluations.md](../evaluations.md).

## Shipped boundary

- `internal/core` defines `Room`, `Record`, `Notification`, and the `Store`
  seam (create room, list rooms, read stream, append record); the conformance
  battery in `contract.go` runs against every Store implementation — SQLite
  always, PostgreSQL when `TEAROOM_TEST_PG` is set, and the HTTP client from
  the transport tests.
- `internal/relational` implements the tree on SQLite (format-3 schema) with
  per-room dense sequences, per-room idempotency ledgers, and the one-time
  format-2 → format-3 migration (old rooms become roots; signatures kept).
- `internal/store` owns the `.tearoom` workspace layout and the format gate.
- `internal/transport` serves the minimal local HTTP face (room list, read,
  append) and the client that implements the same `Store` over it.
- `internal/cli` exposes `room init --name/--parent`, `send`, `read`
  (`--after`, `--tail`, `--subtree`), `tree`, `watch` (`--json`), `server
  start` (loopback), and `skill`. Membership, credentials, and the hosted
  profile are deleted.

## Open work

| Item | State |
|---|---|
| Acceptance journeys S1–S3 (solo delivery, build-and-review, resume) | Hand-run method recorded in [../evaluations.md](../evaluations.md); not yet executed against the room-tree model |
| Wake journey S4 | `watch --json` ships; the external waker script drives herdr (`agent prompt … --wait`). Running it unattended requires each Agent tool's one-time trust dialogs and login to be pre-cleared on the host |
| Two-week real-work validation | The model's own acceptance bar: run the tree as the working context for two weeks of real work |

## Removed with the v1 model (2026-11-07)

Membership, admission, credentials, the hosted multi-room service, the remote
readiness evaluation E7, and the Render/Neon deployment files were deleted.
The rationale is in [../log.md](../log.md); the v1 code is frozen at tag
`v1-final` on branch `archive/v1`.

## Clean-break policy

Releases carry no compatibility layer: no command aliases, no dual schemas,
no old HTTP shapes. Storage upgrades are the one-time format-2 → format-3
migration only. Rollback is an operator action on the Git revision and a data
snapshot — never an in-product compatibility mode.

`make check` (fmt, vet, unit, race, E1/E2 target evaluations, evaluation
registry, line budget, docs bundle) is the completion proof for every change.
