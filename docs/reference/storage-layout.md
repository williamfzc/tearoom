---
type: reference
title: Storage layout
description: The on-disk shape of a workspace and the locking discipline that protects it.
resource: internal/store/file.go
tags: [reference, storage, concurrency]
timestamp: 2026-08-27
---

# Storage layout

```
.tearoom/
  config.json          workspace id, name
  agents/<id>.json     role, last heartbeat
  threads/<id>.json    title, status, owner, events[]
  leases/<hash>.json   resource pattern, holder, expiry
  gates/<id>.json      reason, state, asker, decision
  ledger.jsonl         append-only audit trail
```

Plain files, one object each. Chosen so a human can inspect and repair state with a
text editor, and so nothing needs installing beyond one binary.

## Locking

Every mutation takes an exclusive `flock` on a lock file. Without it, two agents
polling simultaneously both read "free" and both write "held" — the exact bug this
product exists to prevent.

The lock is held for the whole read-modify-write, not just the write, because the
decision to grant a lease depends on state read moments earlier.

See [../lessons/cross-process-locking.md](../lessons/cross-process-locking.md) for how
this is tested, and for the Windows gap.

## The ledger

`ledger.jsonl` is append-only with no delete path. It is what makes an agent run
reviewable after the fact, which is the difference between trusting an agent and being
able to delegate to one.

## Related

- [../domain/concepts.md](../domain/concepts.md) — what these files represent
- [../architecture/layers.md](../architecture/layers.md) — which layer owns this
