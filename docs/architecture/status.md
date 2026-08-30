---
type: architecture
title: Status and roadmap
description: What the executable currently implements, what the minimum Room/Event design requires, and which migration evidence is still missing.
tags: [architecture, status, roadmap]
timestamp: 2026-08-30
---

# Status and roadmap

This status separates shipped behavior from the design baseline in
[model-discussion.md](model-discussion.md). The implementation is still the
older model; no offline Agent activation has shipped.

## Built today

- One `tearoom` binary with `room`, `server`, `event`, and hosted command
  groups.
- Room, Actor, and Event core entities.
- File-backed append-only Event log with cross-process `flock`.
- Relational Store behind `core.Store`, versioned migrations, and SQLite
  selection through `.tearoom/store.json`; PostgreSQL validation is gated.
- Dense Room sequence, idempotent append, required expected cursor, and stale
  write refusal.
- Directed Event filtering and automatic Actor registration on first use.
- HTTP client/server implementing the current Store contract.
- Client-side polling for hosted `event watch`; server-held wait remains an
  optional same-process or LAN optimization.
- Same-product and cross-product session scenarios, harness workflow,
  Werewolf round, cross-worktree Room, and hosted credential matrix.
- Hosted Phase 1 control plane: accounts, memberships, bound hashed
  credentials, revocation, actor binding, and authenticated Room routes. See
  [hosted-plan.md](hosted-plan.md).

These tests and demos prove the current implementation. They do not prove the
target minimum model or offline activation.

## Target design not yet implemented

The current design baseline requires:

- membership as the Room visibility boundary, with every admitted member able
  to read the complete history;
- `to` as public addressing or attention metadata, not secrecy or an implicit
  process-start request;
- repeatable reads with caller-supplied boundaries rather than implicit
  per-Actor cursors;
- ordinary append independent of a hidden or saved read checkpoint;
- stable Actor identity separated from credentials, runtime bindings, and
  ephemeral Agent processes;
- optional activation above the kernel, with explicit durable delivery state
  and at-least-once recovery semantics.

The product and reference documents now define the target, but no migration has
reconciled it with the code, HTTP implementation, integration skill, or
evaluation scripts. The gap is deliberate and must remain visible until a
compatibility plan and implementation exist.

## Deliberately absent

- Offline Agent launcher, runtime binding, activation ledger, lease/retry
  policy, or duplicate-safe wake path.
- Built-in Thread, task, workflow, game, Lease, Gate, or activation entities in
  the Room kernel.
- SSE push or durable subscription state; client polling is the hosted path.
- GitHub OAuth and human sessions; the dev admin token remains a Phase 1 stub.
- Organizations, organization membership, and billing.
- Hostile-network controls such as application rate limiting; TLS termination
  is expected in front of the hosted service.
- Log compaction and snapshots.

## Evidence required next

1. Specify a reversible migration from the current cursor and
   directed-visibility contract to the adopted minimum Room/Event model.
2. Prove that Room-wide membership visibility and explicit read boundaries work
   across local, hosted, and cross-worktree journeys.
3. Build one local activation vertical slice: an offline member is addressed,
   a durable activation is claimed, the process starts after restart, and the
   Agent reads the referenced Event and appends a response.
4. Test at-least-once activation and duplicate external effects separately;
   an invocation ending is not proof that collaboration succeeded.
5. Measure whether replay of Events, activation attempts, decisions, and
   artifact references changes a real human-Agent workflow before building a
   full workspace UI.
6. Validate SQLite and PostgreSQL after the model migration, not only against
   the current cursor-based schema.

## Completion proof

`make check` currently runs unit tests, binary smoke suites, session handoffs,
workflow and game demos, cross-worktree and hosted demos, evaluation
consistency, and documentation validation. Its green result is evidence for
the old behavior. A future model migration needs new boundary tests and a
separate activation journey before this status can say the target is built.

## Related

- [model-discussion.md](model-discussion.md)
- [convergence.md](convergence.md)
- [hosted-plan.md](hosted-plan.md)
- [../evaluations.md](../evaluations.md)
- [../competitive-analysis.md](../competitive-analysis.md)
