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
  battery in `contract.go` runs against every Store implementation — the
  JSONL store always and the HTTP client from the transport tests.
- `internal/store` implements the tree as JSONL files under `.tearoom`
  (format 4): one directory and one append-only stream per Room, identity in
  `room.json`, per-(author, key) retry ledgers, and a cross-process flock
  around every append. The one-time format-2/3 → 4 migrations read the
  retired SQLite layouts and rewrite the workspace as files.
- `internal/transport` serves the minimal local HTTP face (room list, read,
  append) and the client that implements the same `Store` over it.
- `internal/cli` exposes `room init --name/--parent`, `send`, `read`
  (`--after`, `--tail`, `--subtree`), `tree`, `watch` (`--json`), `server
  start` (loopback), and `skill`. Membership, credentials, and the hosted
  profile are deleted.
- The repository carries its own workspace in `.tearoom/` and works inside
  it: the project's goal and acceptance live in the root Room.

## Open work

The group-chat and hc integration in [../product.md](../product.md) is
implemented locally; its operational limits and remaining acceptance work
are tracked below. The delivery contract is in [collaboration.md](collaboration.md).

| Item | State |
|---|---|
| Acceptance journeys S1–S3 (solo delivery, build-and-review, resume) | One unattended Pi–Claude build-and-review journey passed; next-day resume and the complete acceptance suite remain open. See [../evaluations.md](../evaluations.md) |
| Wake journey S4 | The hc dispatcher delivers addressed records to Room-role panels; unattended operation requires each Agent tool's one-time trust dialogs and login to be pre-cleared on the host. See [agent integration](../reference/agent-integration.md) for setup and [evaluations](../evaluations.md) for evidence |
| Group collaboration | Local role definitions, Room rosters, addressed records and an hc dispatcher are implemented; one unattended two-Agent journey passed. Signatures are unverified, so independent review cannot be established from Room records alone; the evidence includes both Agent panels. See [../evaluations.md](../evaluations.md) |
| Two-week real-work validation | The model's own acceptance bar: run the tree as the working context for two weeks of real work |

## Removed with the v1 model (2026-11-07)

Membership, admission, credentials, the hosted multi-room service, the remote
readiness evaluation E7, and the Render/Neon deployment files were deleted.
The SQL backends (SQLite Store, the PostgreSQL adapter, and their gated
battery) were removed the same day by the JSONL cutover; their validation
runs are recorded in [../evaluations.md](../evaluations.md). The rationale
for both moves is in [../log.md](../log.md); the v1 code is frozen at tag
`v1-final` on branch `archive/v1`.

## Clean-break policy

Releases carry no compatibility layer: no command aliases, no dual schemas,
no old HTTP shapes. Storage upgrades are the one-time format-2/3 → format-4
migrations only. Rollback is an operator action on the Git revision and a data
snapshot — never an in-product compatibility mode.

`make check` (fmt, vet, unit, race, E1/E2 target evaluations, evaluation
registry, line budget, docs bundle) is the completion proof for every change.
