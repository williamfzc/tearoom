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

- `rust/src/core.rs` defines the Room and Record data, content compaction,
  and the idempotency fingerprint. The room-tree contract is proven by the
  unit tests and the E1/E2 target evaluations through the shipped binary.
- `rust/src/store.rs` and `store/` implement the tree as JSONL files under
  `.tearoom` (format 4): one directory and one append-only stream per Room,
  identity in `room.json`, per-(author, key) retry ledgers, crash recovery
  through the pending-intent file, and a cross-process flock around every
  append. Legacy format-2/3 (SQLite) workspaces are refused on open — the
  one-time migration belongs to the final Go release (tag `go-final`).
- `rust/src/cli.rs` and `cli/` expose `room init --name/--parent`, `send`,
  `read` (`--after`, `--tail`, `--subtree`), `tree`, `skill`, and the
  `role`/`collab` collaboration surface. Membership, credentials, the hosted
  profile, and the watch/HTTP faces are deleted (2026-11-09 concept pass;
  see [../log.md](../log.md)).
- The repository works inside its own `.tearoom/` workspace, which stays
  local and untracked (owner decision 2026-11-09): the project's goal and
  acceptance live in the root Room on this machine.

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
no old HTTP shapes. Storage changes are one-time cutovers to a new format
number; retired formats are refused rather than carried, and the 2026-11-08
Rust cutover retired the Go implementation with it (preserved at tag
`go-final`, refused as a parallel tree). Rollback is an operator action on
the Git revision and a data snapshot — never an in-product compatibility
mode.

`make check` (formatting, Clippy, unit tests, E1/E2 target evaluations,
evaluation registry, line budget, docs bundle, release-package checks) is
the completion proof for every change.
