---
type: architecture
title: Status and roadmap
description: What exists at M0, what is deliberately absent, and the hypothesis each future phase tests.
tags: [architecture, status, roadmap]
timestamp: 2026-08-27
---

# Status and roadmap

## Built

M0, working end to end:

- `tea` — the agent-facing CLI, ten commands, `--json` on all of them
- `tea-server` — HTTP access to a workspace, plus the console assets
- the console — Go compiled to WebAssembly, rendering the same resolved board the CLI prints
- a file-backed store with cross-process locking and an append-only ledger

## Deliberately absent

- No realtime push. Agents poll through blocking `wait`.
- No enforcement of leases at the filesystem level. They are advisory by design — see
  [../domain/concepts.md](../domain/concepts.md).
- No auto-approval of gates, ever.
- No write surface in the console beyond approve and reject.
- No hosted multi-tenant store yet, though the `Store` interface exists so one can drop in.

## Roadmap

| Phase | Scope | Hypothesis under test | Pass condition |
|---|---|---|---|
| M0 | CLI, local store, threads, leases, gates | Can agents hand off without a human relay? | A real multi-agent task with zero copy-paste |
| M1 | MCP adapter, read-only console | Can a human grasp cluster state at a glance? | 7 of 10 users answer "who is stuck" within 10s |
| M2 | Gates, the human write actions, IM forwarding | Is the human-in-the-loop path actually used? | Gate usage and response time land in an acceptable band |
| M3 | Hosted store, billing, team permissions | Will anyone pay for coordination alone? | First paid conversions with renewal intent |

The MCP adapter will be the third client of the store interface. Everything it needs
must already be enforced below that interface — see
[../lessons/invariants-belong-in-the-store.md](../lessons/invariants-belong-in-the-store.md).

## Risks

| Risk | Assessment | Response |
|---|---|---|
| Agent vendors ship this natively | The existential one | Bet on cross-vendor neutrality; vendors coordinate only within their own ecosystem |
| "I'll just write a script" | Legitimate — the local case really is ~100 lines | Value must concentrate where scripts fail: multi-machine, teams, console, audit, crash recovery |
| Multi-agent coordination is a fake need | Plausible if single-agent capability keeps improving | Gate on dogfooding: if we don't want to use it, stop |
| Advisory leases are too weak | Agents can ignore them | Optional git pre-commit guard as a second line; no kernel enforcement |

## Related

- [../domain/open-questions.md](../domain/open-questions.md) — decisions still unsettled
- [layers.md](layers.md) — the structure this roadmap must not violate
