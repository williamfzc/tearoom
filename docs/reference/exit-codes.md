---
type: reference
title: Exit codes
description: The five exit statuses tea returns and why they are distinct.
resource: internal/cli/cli.go
tags: [reference, contract, cli, agents]
timestamp: 2026-08-27
---

# Exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | usage or validation error |
| 2 | conflict — the resource is held by someone else |
| 3 | timeout — `wait` expired without a change |
| 4 | store unavailable |

## Why they are distinct

Agents branch on exit status without parsing prose. Collapsing these into a single
non-zero code would force text matching, which breaks the moment a message is reworded.

Two distinctions carry most of the weight:

**2 is not a failure.** A rejected `claim` is a normal, expected outcome — the resource
is busy, try later or pick different work. Treating it as a crash would make agents give
up on healthy contention.

**4 must never be mistaken for 0.** An unreachable store means the workspace state is
unknown. Continuing as though a lease were held is how silent overwrites happen, so
there is no degraded mode and no cached fallback. See
[../domain/boundaries.md](../domain/boundaries.md).

## Guarded by

`scripts/smoke.sh` asserts each code against the real binary, including a conflicting
claim (2), a missing workspace (4), and an unreachable server (4).

## Related

- [cli.md](cli.md) — the commands that return these
- [../domain/boundaries.md](../domain/boundaries.md) — the reasoning behind failing loudly
