---
type: reference
title: Exit codes
description: Target machine-readable CLI outcomes and current compatibility notes.
resource: internal/cli/cli.go
tags: [reference, contract, cli, agents]
timestamp: 2026-08-30
---

# Exit codes

## Target contract

| Code | Meaning |
|---|---|
| 0 | Operation succeeded |
| 1 | Invalid command, validation, authentication, or unknown Actor |
| 3 | `read --wait` elapsed without a later Event |
| 4 | Local Room or remote service unavailable |

The target contract has no mandatory stale-write precondition, so stale cursor conflict is not a target exit class. A future conditional-append protocol may define a separate conflict outcome only when a real caller requires it.

## Current implementation note

The shipped CLI also returns code `2` for a stale cursor or idempotency-key collision, and its timeout is attached to `event watch`. Code `2` is compatibility behavior while the implementation still follows the superseded cursor-bound Store contract; it is not evidence for the target model.

Agents should branch on the documented code rather than parse prose. Code `4` must never be treated as success because Room state is unknown.

The current assertions live in [scripts/smoke.sh](../../scripts/smoke.sh); they cover the shipped implementation and must be rewritten before they can prove the target contract.
