---
type: reference
title: Exit codes
description: Authoritative machine-readable CLI outcomes.
resource: internal/cli/cli.go
tags: [reference, contract, cli, agents]
timestamp: 2026-08-30
---

# Exit codes

## Contract

| Code | Meaning |
|---|---|
| 0 | Operation succeeded; an empty `read` is still success |
| 1 | Invalid command, validation, unknown Actor, or any store/HTTP error — including an HTTP 409 idempotency conflict |
| 3 | `read --wait` elapsed without a later Event |
| 4 | No local Room (run `tearoom room init`) or the server is unreachable |

There is no mandatory stale-write precondition, so no separate stale-cursor
exit class exists. A future conditional-append protocol may define a separate
conflict outcome only when a real caller requires it.

Agents should branch on the documented code rather than parse prose. Code `4`
must never be treated as success because Room state is unknown.

Executable proof belongs in [evaluations.md](../evaluations.md). No legacy
exit class is reserved for compatibility.
