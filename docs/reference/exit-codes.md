---
type: reference
title: Exit codes
description: Stable exit statuses returned by tearoom.
resource: internal/cli/cli.go
tags: [reference, contract, cli, agents]
timestamp: 2026-08-28
---

# Exit codes

| Code | Meaning |
|---|---|
| 0 | operation succeeded |
| 1 | invalid command, validation error, or unknown Actor |
| 2 | conflict: stale cursor or idempotency-key collision |
| 3 | `event watch` timed out without Room movement |
| 4 | local Room or remote Store unavailable |

Agents may branch on these values without parsing prose. Exit 2 is an expected coordination outcome: run `event list` before deciding whether to retry. Exit 4 must never be treated as success because Room state is unknown.

`scripts/smoke.sh` asserts every non-zero class against the shipped binary.
