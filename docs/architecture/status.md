---
type: architecture
title: Status and roadmap
description: What the Room, Actor, Event kernel implements and what remains outside it.
tags: [architecture, status, roadmap]
timestamp: 2026-08-28
---

# Status and roadmap

## Built

- One `tearoom` binary with `room`, `server`, and `event` command groups.
- Room, Actor, Event as the only core entities.
- File-backed append-only Event log with cross-process `flock`.
- Dense Room sequence, required expected cursor, and stale-write refusal.
- Idempotent appends and directed Event filtering.
- HTTP client and server implementing the same Store contract.
- Executable same-product and cross-product multi-session scenarios.
- Server-held wait for `event watch`: a long-poll request wakes on the first visible Event and costs nothing while a Room is quiet.
- Relational Store behind `core.Store`: GORM models, versioned migrations, and one append transaction that enforces the same cursor, idempotency, admission, and visibility invariants as `flock` — shipped on SQLite, selected per Room by `--store sqlite` and dispatched from `.tearoom/store.json`.
- Multi-tenant hosted server (Phase 1, [hosted-plan.md](hosted-plan.md)): a control plane (`ctl_` tables: accounts, memberships, credentials) in front of the unchanged coordination schema; per-room `relational.Store` value objects over one shared database; credentials as hashed, bound, revocable tokens; the event API at `/r/<room-id>/api/*` gated by a Bearer middleware; management routes under `/api/hosted/*` gated by a dev admin token until the GitHub OAuth exchange lands.

## Deliberately absent

- Browser or WebAssembly console.
- Built-in Thread, Lease, Gate, task, workflow, or game entities.
- SSE push; watch returns batches through long-poll instead of streaming.
- GitHub OAuth and human sessions; the dev admin token is a Phase 1 stub.
- Organizations, organization membership, and billing.
- PostgreSQL validation executed against a running PostgreSQL; the gated suite (`make pg-smoke`, `TEAROOM_TEST_PG`) is shipped but skipped without one.
- Hostile-network security: the hosted server assumes TLS termination in front of it and has no rate limiting yet.
- Log compaction and snapshots.

## Next evidence to collect

1. Use one Room across several real worktrees and measure whether cursor-only reads are sufficient. **Shipped 2026-08-29** as the worktree demo (story S4, evaluation E7): two real git worktrees share one Room served from the repository root, handoffs cross worktrees by `watch`, later processes resume their cursors, and a fresh Actor reads the full trace through one Room.
2. Implement one task protocol and one game protocol outside the kernel to test whether the three entities remain enough. **Both shipped 2026-08-29**: the task protocol as the harness workflow scenario (story S2), and the game protocol as one Werewolf round over directed role Events (story S3, evaluation E6) — a moderator deals private roles, night actions reach only the moderator and their sender, and the day is public.
3. Measure irrelevant Event volume before adding subscriptions, mute, or indexing.
4. Validate the SQLite schema and migrations on PostgreSQL before a hosted release. **Gated suite shipped 2026-08-29**: `make pg-smoke` and the `TEAROOM_TEST_PG` unit battery run the same invariants against a real PostgreSQL; both skip with a notice until a DSN is provided.
5. Build the hosted identity boundary described in [../domain/hosted-identity.md](../domain/hosted-identity.md) before exposing Rooms to untrusted clients. **Phase 1 shipped 2026-08-29** ([hosted-plan.md](hosted-plan.md)): credentials, actor binding, session resume, and revocation are proven by evaluation E8; the GitHub sign-in exchange (S5) is the remaining open piece.

Items 1 and 2 are the open stories S4 and S3 in the [evaluation set](../evaluations.md), item 5 is the hosted stories S5–S10, and items 3–4 are tracked there as pending evidence rather than stories.

## Near-term priorities

[../competitive-analysis.md](../competitive-analysis.md) set three: a server-held wait for `event watch` (shipped as long-poll), the relational Store beginning with SQLite (shipped as `--store sqlite`), and one cross-agent skill over the CLI before any MCP server (shipped as the Tearoom skill).

## Completion proof

`make check` runs unit tests, the shipped-binary smoke suites for both backends, same-product and cross-product session handoffs, the harness workflow scenario, the Werewolf game round, the cross-worktree Room, the hosted credential matrix, stale raw HTTP rejection, the evaluation-set consistency check, and documentation validation. The PostgreSQL suite runs inside the same chain wherever a database exists.
