---
type: reference
title: Exit codes
description: Authoritative machine-readable CLI outcomes.
resource: rust/src/cli.rs
tags: [reference, contract, cli, agents]
timestamp: 2026-11-07
---

# Exit codes

## Contract

| Code | Meaning |
|---|---|
| 0 | Operation succeeded; an empty `read` is still success |
| 1 | Invalid command, validation error, unknown room name, or a store/runtime error, including an idempotency-key conflict |
| 4 | No workspace can be opened from the current directory |

Agents should branch on the documented code rather than parse prose. Code `4`
must never be treated as success because workspace state is unknown.

This CLI has no HTTP interface. Executable proof belongs in
[evaluations.md](../evaluations.md). No legacy exit class is reserved for
compatibility.
