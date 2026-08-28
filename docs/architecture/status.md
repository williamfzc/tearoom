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

- `tea` — the agent-facing CLI, fourteen commands, `--json` on all of them; the surface is
  not converged, and [convergence.md](convergence.md) has the audit that cuts it to ten
- `tea-server` — HTTP access to a workspace, plus the console assets
- the console — Go compiled to WebAssembly, rendering the same resolved board the CLI prints
- a file-backed store with cross-process locking and an append-only ledger

## Known gaps, not decisions

Found on 2026-08-28 by checking this binary against consensus systems. These are defects
awaiting work, listed separately from the choices below so the two are not confused.

- **Stale writes are accepted.** No freshness check on commit; an agent reasoning from an
  old snapshot can write a false statement into a thread. The severe one.
- **`claim` does not take ownership of its thread**, so a held lease can coexist with a
  board that says the work is unowned.
- **Conflicts state the obstacle, not the remedy**, and ignore `--json` on that path.
- **Errors cannot separate effects that landed from effects that did not**, which invites
  a retry to repeat the half that succeeded.
- **The whole attention axis is missing** — no notice tier, no mute, no deferred intent, no
  durable session. See [../domain/agent-experience.md](../domain/agent-experience.md).

Details and sources in
[../lessons/borrowed-from-consensus.md](../lessons/borrowed-from-consensus.md).

## Deliberately absent

- No realtime push. Agents poll through blocking `wait` — which costs a held tool call and
  cannot outlive the session; see [../domain/agent-experience.md](../domain/agent-experience.md).
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
| The lease niche is already crowded | Confirmed — several open-source projects do exactly leases-for-agents | Differentiate on the attention axis, which none of them touch; see [../domain/agent-experience.md](../domain/agent-experience.md) |
| Advisory leases are too weak | Agents can ignore them | Optional git pre-commit guard as a second line; no kernel enforcement |

## Related

- [../domain/open-questions.md](../domain/open-questions.md) — decisions still unsettled
- [layers.md](layers.md) — the structure this roadmap must not violate
