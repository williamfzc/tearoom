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

- `src/core.rs` defines the Room and Record data, content compaction,
  and the idempotency fingerprint. The room-tree contract is proven by the
  unit tests and the E1/E2 target evaluations through the shipped binary.
- `src/store.rs` and `store/` implement the tree as JSONL files under
  `.tearoom` (format 4): one directory and one append-only stream per Room,
  identity in `room.json`, per-(author, key) retry ledgers, crash recovery
  through the pending-intent file, and a cross-process flock around every
  append. Legacy format-2/3 (SQLite) workspaces are refused on open — the
  one-time migration belongs to the final Go release (tag `go-final`).
- `src/cli.rs` and `cli/` expose `room init --name/--parent/--description`,
  `room update` (edits the display name and description labels), `room show`,
  `room delete --confirm`, `send`, `read` (`--after`, `--tail`, `--subtree`),
  `tree`, `skill`, and the `role`/`collab` collaboration surface,
  including the per-user `service status|stop|forget` commands. Rooms carry
  an immutable id (their birth directory name) that references and state
  anchor to; label editing cannot break history (see
  [storage layout](../reference/storage-layout.md)).
  Membership, credentials, the hosted profile, and the watch/HTTP faces are
  deleted (2026-11-09 concept pass; see [../log.md](../log.md)).
- The `app/` Tauri desktop app is the optional local face for Room
  navigation, record history, and addressed text append. It reads through the
  same Store as the CLI and sends through the CLI `send` command; command
  behavior is tested under `make test` and the UI journey is hand-run
  evidence (see [../evaluations.md](../evaluations.md)). The 2026-11-10
  loopback browser face was removed with it (see [../log.md](../log.md)).
- A repository-local `.tearoom/` workspace is optional and untracked. Existing
  workspace data is preserved; repository work does not require a root Room.

## Open work

The group-chat and hc integration in [../product.md](../product.md) is
implemented locally; its operational limits and remaining acceptance work
are tracked below. The delivery contract is in [collaboration.md](collaboration.md).

| Item | State |
|---|---|
| Local collaboration service | The Unix implementation starts one per-user service on the first addressed send or explicit `collab run`, registers multiple workspaces, and exposes `service status|stop|forget`. Pure reads and Room creation do not start it. Nine scripted service integration cases passed on macOS arm64, including a stop followed by a new addressed send. They remain ignored in the regular suite because they start an isolated hc service. Passing cases cover CLI exit, multiple workspaces, cleanup of owned terminals while preserving unrelated ones, service-terminal SIGKILL, and uncertain-delivery recovery. Linux, hc-server failure, real Agent processes, detached descendants, and identity-check races remain unverified. See [service lifecycle and limits](collaboration.md#local-collaboration-service). |
| Acceptance journeys S1–S3 (solo delivery, build-and-review, resume) | One unattended Pi–Claude build-and-review journey passed; next-day resume and the complete acceptance suite remain open. See [../evaluations.md](../evaluations.md) |
| Wake journey S4 | The hc dispatcher delivers addressed records to Room-role panels; unattended operation requires each Agent tool's one-time trust dialogs and login to be pre-cleared on the host. See [agent integration](../reference/agent-integration.md) for setup and [evaluations](../evaluations.md) for evidence |
| Wake delivery | New Room records are sent directly, and only explicit recipients are woken. A finished turn releases the next queued delivery without requiring a reply. One Trae CLI Agent continued across wakes with new records; a broader multi-Agent journey remains open. Reliable lifecycle status remains required. See [collaboration delivery](collaboration.md#delivery-and-recovery) and [evidence](../evaluations.md#hand-run-evidence). |
| Group collaboration | Local role definitions, Room rosters, addressed records and an hc dispatcher are implemented; one unattended two-Agent journey passed. Nine scripted service integration cases passed on macOS arm64, including service-terminal SIGKILL, stop and restart, and uncertain-delivery recovery. These cases are not part of the regular check. The service marks submitted deliveries unknown after restart, and normal stop targets identity-labeled terminals. Linux, hc-server failure, and deliberately detached processes remain unverified. Signatures are unverified, so independent review cannot be established from Room records alone; the evidence includes both Agent panels. See [collaboration.md](collaboration.md#local-collaboration-service) and [../evaluations.md](../evaluations.md) |
| Conversation continuity and replacement | An existing ready hc panel receives later messages and a busy panel queues them; a native interactive DSH terminal kept one conversation across two dispatched wakes in a Room. A previously used missing panel now requires reconciliation instead of automatically starting a new conversation. `collab restart` explicitly starts a new conversation with Room history; Tearoom cannot resume an identified Agent session. A new terminal UI can report `idle` before its setup ends, leaving delivery uncertain. See [collaboration delivery](collaboration.md#conversation-continuity-and-replacement) |
| Cross-product collaboration | One direct-command Pi-to-Codex handoff passed with operator-assisted setup and startup. Claude Code lacks reliable hc lifecycle status here. A community DSH terminal profile received two Tearoom deliveries in one Room and needed its first-run screens cleared by an operator; broader Agent combinations and an unattended cross-product journey remain unproven. See [../evaluations.md](../evaluations.md) |
| Herdr visibility | Each Room now uses a separate named local Herdr session, with role panels in tabs. Two real Rooms and DSH roles were checked; Herdr's independently selected remote machine can still obscure the local view. Restart limits are in [agent integration](../reference/agent-integration.md#herdr-session-per-room) and evidence is in [evaluations](../evaluations.md) |

## Removed with the v1 model (2026-11-07)

Membership, admission, credentials, the hosted multi-room service, the remote
readiness evaluation E7, and the Render/Neon deployment files were deleted.
The SQL backends (SQLite Store, the PostgreSQL adapter, and their gated
battery) were removed the same day by the JSONL cutover; their validation
runs are recorded in [../evaluations.md](../evaluations.md). The rationale
for both moves is in [../log.md](../log.md); the v1 code is preserved at tag
`v1-final`.

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
