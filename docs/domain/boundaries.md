---
type: domain
title: Boundary behaviour
description: Target and current compatibility behaviour under retries, contention, crashes, and unavailable stores.
tags: [domain, failure-modes, invariants]
timestamp: 2026-08-30
---

# Boundary behaviour

The target Room contract keeps durable history explicit and makes stronger
delivery or freshness guarantees opt-in. The current implementation still has
several older compatibility behaviours; they are marked below.

| Situation | Target behaviour | Current implementation status |
|---|---|---|
| Room changes after an actor read | Ordinary append remains valid; a protocol may request an explicit conditional append | Current Store still rejects expected-cursor writes; migration pending |
| Identical request is retried | An idempotent append capability may return the original Event | Current implementation supports the older key contract |
| Writers race | Store serialization assigns distinct dense sequence numbers | File/relational implementations still combine this with stale-write checks |
| Writer crashes while holding the file lock | The kernel releases `flock` | Implemented for Unix-like local use |
| Store or server is unavailable | Command fails; no cached success is claimed | Implemented compatibility behaviour |
| Reader supplies a boundary | Read repeats from the caller's explicit `--after`/`--since` boundary | CLI still uses a saved session cursor; migration pending |
| Any Room member reads history | The complete Room history is visible; `to` does not filter it | Current Store still filters directed Events; migration pending |
| A Room needs privacy | Use a separate Room with explicit membership | Current local auto-registration does not yet provide this boundary |
| An addressed Agent is offline | Optional activation may record durable delivery state and retry a launcher; Room alone does not wake it | No durable activation service is shipped |

## What is not a read checkpoint

A Room does not contain hidden Events in the target model. `seq` identifies an
Event in the shared history; it is not a per-Actor read checkpoint. A future
durable delivery consumer must own an explicit consumer/subscription identity
and delivery ledger rather than borrowing Actor read state.

## Windows

The current cross-process lock is implemented for Unix-like systems. Windows builds do not yet provide a real lock and are not safe for concurrent use.
