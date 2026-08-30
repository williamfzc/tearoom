---
type: index
title: Lessons
description: Durable rules learned while building the Room kernel.
tags: [index, lessons]
timestamp: 2026-08-30
---

# Lessons

| Document | Rule |
|---|---|
| [invariants-belong-in-the-store.md](invariants-belong-in-the-store.md) | A shared rule lives below every client |
| [cross-process-locking.md](cross-process-locking.md) | Verify filesystem coordination with independent processes |
| [flag-parsing.md](flag-parsing.md) | Accept the argument order callers naturally emit |
| [borrowed-from-consensus.md](borrowed-from-consensus.md) | Order once, make retries safe, and use freshness only for real protocol preconditions |

These are retained because the wrong implementation can look correct until a second client or process appears.
