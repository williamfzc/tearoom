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

## Deliberately absent

- Browser or WebAssembly console.
- Built-in Thread, Lease, Gate, task, workflow, or game entities.
- Realtime push; `event watch` polls.
- Hosted multi-tenant service.
- Authentication and hostile-network security.
- Log compaction and snapshots.

## Next evidence to collect

1. Use one Room across several real worktrees and measure whether cursor-only reads are sufficient.
2. Implement one task protocol and one game protocol outside the kernel to test whether the three entities remain enough.
3. Measure irrelevant Event volume before adding subscriptions, mute, or indexing.
4. Define authentication before exposing a Room beyond a trusted machine or network.

## Completion proof

`make check` runs unit tests, the shipped-binary smoke suite, same-product and cross-product session handoffs, stale raw HTTP rejection, and documentation validation.
