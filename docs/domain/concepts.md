---
type: domain
title: The five concepts
description: Canonical definitions of workspace, agent, thread, lease and gate, with the invariants each carries.
resource: internal/core/core.go
tags: [domain, concepts, invariants]
timestamp: 2026-08-27
---

# The five concepts

Defined in `internal/core`. This document is the canonical description; the code is
the canonical implementation.

## Workspace

A project or repository, and the unit of isolation. Bound to a directory through
`.tearoom/config.json` but not strictly tied to it.

Granularity is unsettled — see [open-questions.md](open-questions.md).

## Agent

A durable identity with a role label and a heartbeat.

**Identity comes from the token, not the process.** An agent that crashes and restarts
is the same agent. This is what makes lease reclamation and staleness detection
meaningful: without stable identity, a crash would look like a new participant and the
lease it was holding could never be safely reclaimed.

## Thread

One goal, with an append-only event log and a fixed state machine:

```
open ──▶ claimed ──▶ resolved
 │  ▲       │  ▲
 ▼  │       ▼  │
blocked ────┘  │
 └─────────────┘
```

Transitions are free-form in both directions with one exception: reaching `resolved`
requires at least one event carrying evidence. Claiming something is done is not the
same as showing it.

There are no custom states. A human scanning a board should not have to learn
per-project vocabulary before they can tell whether work is moving.

## Lease

An advisory exclusive hold on a resource — a file glob, a port, a task name — with a
mandatory TTL.

**Advisory, not enforced.** An agent can ignore a lease and write anyway. Enforcing at
the filesystem level would need a kernel hook or a FUSE mount: far heavier than a
single binary to install, and still bypassable. The optional git pre-commit guard is
the second line of defence.

Two invariants matter more than the rest:

- Two agents in separate processes calling `claim` at the same instant must not both
  succeed. See [../architecture/layers.md](../architecture/layers.md) for where this is
  enforced and [../lessons/cross-process-locking.md](../lessons/cross-process-locking.md)
  for how it is tested.
- An expired lease is not a lease. Filtering happens in `internal/resolve` so no
  caller has to remember this.

Default TTL is unsettled — see [open-questions.md](open-questions.md).

## Gate

A checkpoint requiring human approval. Blocks `wait` until settled.

**There is no auto-approval on timeout.** A gate that approves itself after an hour is
not a gate; auto-approval destroys the only reason the concept exists.

Two rules that live in the store rather than in any client:

- Opening a gate blocks its thread. Deciding it moves the thread out of blocked —
  approved resumes as `claimed`, rejected returns to `open` so a different approach can
  be tried.
- Approval resumes the thread as the agent that **asked**, not the human who approved.
  A human pressing a button is not volunteering to do the work.

Both were originally implemented in the CLI, with consequences recorded in
[../lessons/invariants-belong-in-the-store.md](../lessons/invariants-belong-in-the-store.md).

## Related

- [problem.md](problem.md) — the failure modes these concepts address
- [boundaries.md](boundaries.md) — how each behaves under crash and contention
- [../reference/cli.md](../reference/cli.md) — the commands that manipulate them
