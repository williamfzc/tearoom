---
type: index
title: Lessons
description: Durable engineering lessons supporting the target Room kernel.
tags: [index, lessons]
timestamp: 2026-08-30
---

# Lessons

Lessons preserve reasoning that supports the target design. They do not replace
[product.md](../product.md), the implementation-gap record in
[architecture/status.md](../architecture/status.md), the target contracts in
[reference/](../reference/index.md), or the proof registry in
[../evaluations.md](../evaluations.md). A lesson that describes a legacy
implementation is evidence for migration, not a normative contract.

| Document | Rule |
|---|---|
| [invariants-belong-in-the-store.md](invariants-belong-in-the-store.md) | A shared rule lives below every client |
| [cross-process-locking.md](cross-process-locking.md) | Verify filesystem coordination with independent processes |
| [flag-parsing.md](flag-parsing.md) | Accept the argument order callers naturally emit |
| [borrowed-from-consensus.md](borrowed-from-consensus.md) | Order once, make retries safe, and use freshness only for real protocol preconditions |

These are retained because the wrong implementation can look correct until a second client or process appears.
