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

## Deliberately absent

- Browser or WebAssembly console.
- Built-in Thread, Lease, Gate, task, workflow, or game entities.
- SSE push; watch returns batches through long-poll instead of streaming.
- Hosted multi-tenant service and management interface.
- GitHub login, Room ownership, membership, Agent credentials, and billing.
- PostgreSQL validation of the relational Store; the SQLite backend is shipped.
- Authentication and hostile-network security.
- Log compaction and snapshots.

## Next evidence to collect

1. Use one Room across several real worktrees and measure whether cursor-only reads are sufficient.
2. Implement one task protocol and one game protocol outside the kernel to test whether the three entities remain enough. The task protocol shipped 2026-08-29 as the harness workflow scenario; the game protocol (for example Werewolf with private role events) remains to run.
3. Measure irrelevant Event volume before adding subscriptions, mute, or indexing.
4. Validate the SQLite schema and migrations on PostgreSQL before a hosted release.
5. Build the hosted identity boundary described in [../domain/hosted-identity.md](../domain/hosted-identity.md) before exposing Rooms to untrusted clients.

Items 1 and 2 are the open stories S4 and S3 in the [evaluation set](../evaluations.md), item 5 is the deferred stories S5–S10, and items 3–4 are tracked there as pending evidence rather than stories.

## Near-term priorities

[../competitive-analysis.md](../competitive-analysis.md) set three: a server-held wait for `event watch` (shipped as long-poll), the relational Store beginning with SQLite (shipped as `--store sqlite`), and one cross-agent skill over the CLI before any MCP server (shipped as the Tearoom skill).

## Completion proof

`make check` runs unit tests, the shipped-binary smoke suites for both backends, same-product and cross-product session handoffs, the harness workflow scenario, stale raw HTTP rejection, the evaluation-set consistency check, and documentation validation.
