---
type: domain
title: Boundary behaviour
description: What Tearoom does at the edges — crashes, unreachable stores, contention, unbounded growth, ignored gates and circular waits.
tags: [domain, failure-modes, invariants]
timestamp: 2026-08-27
---

# Boundary behaviour

Every row is a decision about what to do when something goes wrong. The common thread:
prefer a loud failure to a quiet false belief.

| Situation | Behaviour | Rationale |
|---|---|---|
| Agent crashes holding a lease | Lease becomes preemptable once the heartbeat is stale; preemption is written to the ledger | Prefer progress over deadlock, but never silently |
| Store unreachable | Command fails loudly with exit 4, no degraded mode | A false lease is more dangerous than a failed command |
| Two agents claim the same resource | Serialised by the lock; the loser is told the holder and the expiry | A rejection must be actionable, not just a refusal |
| Thread grows unboundedly | `context` compacts by default | The cheap path must be the default path |
| Human ignores a gate | Blocks indefinitely, escalates reminders | Auto-approval defeats the entire purpose |
| Circular waits | Detect the cycle, surface it as a deadlock, require a human | Auto-breaking picks a victim, and picking wrong loses work |

## Why failure is loud

An agent that believes it holds a lease it does not hold is worse than an agent that
crashed. The crash is visible; the false belief produces a silent overwrite later,
somewhere else, with no trace connecting the two.

This is why there is no offline mode and no cached lease state. See
[../reference/exit-codes.md](../reference/exit-codes.md) for how this surfaces to
callers.

## Why deadlocks are reported, not resolved

Breaking a cycle automatically means choosing which agent loses its work. The
information needed to choose correctly — which branch is more valuable, which is
nearly done — is not available to the tool. So the cycle is named, both sides are
identified, and a human decides.

## Related

- [concepts.md](concepts.md) — the invariants these boundaries protect
- [../reference/exit-codes.md](../reference/exit-codes.md) — the contract agents branch on
- [../lessons/cross-process-locking.md](../lessons/cross-process-locking.md) — how serialisation is verified
