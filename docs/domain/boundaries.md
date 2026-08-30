---
type: domain
title: Boundary behaviour
description: Authoritative Room behaviour under retries, contention, crashes, and unavailable stores.
tags: [domain, failure-modes, invariants]
timestamp: 2026-08-30
---

# Boundary behaviour

The Room contract keeps durable history explicit and makes stronger delivery
or freshness guarantees opt-in. Implementation gaps belong only in
[status.md](../architecture/status.md).

| Situation | Behaviour |
|---|---|
| Room changes after an Actor read | Ordinary append remains valid; a protocol may request an explicit conditional append |
| Identical request is retried | An idempotent append capability may return the original Event |
| Writers race | Store serialization assigns distinct dense sequence numbers |
| Writer crashes while holding a file lock | The operating system releases the lock; no partial Event is accepted |
| Store or server is unavailable | Command fails; no cached success is claimed |
| Reader supplies a boundary | Read repeats from the caller's explicit `--after e<seq>` boundary |
| Any Room member reads history | The complete Room history is visible; `to` does not filter it |
| A Room needs privacy | Use a separate Room with explicit membership |
| An addressed Agent is offline | Optional activation may record durable delivery state and retry a launcher; Room alone does not wake it |

## What is not a read checkpoint

A Room does not contain hidden Events in the target model. `seq` identifies an
Event in the shared history; it is not a per-Actor read checkpoint. A future
durable delivery consumer must own an explicit consumer/subscription identity
and delivery ledger rather than borrowing Actor read state.

## Platform rule

A backend must provide a real cross-process serialization primitive or reject
concurrent operation on that platform. A no-op lock is never acceptable.
