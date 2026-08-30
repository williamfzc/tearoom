---
type: evaluations
title: Evaluation set
description: Target user stories and the evidence required to prove the Room contract.
tags: [evaluations, stories, testing, registry]
timestamp: 2026-08-30
---

# Evaluation set

This registry describes the target contract in [model-discussion.md](architecture/model-discussion.md), not every behavior currently shipped by the binary. A target story is `open` until an evaluation exercises the target semantics. `proven` is reserved for an evaluation that is both executable and aligned with that contract; `deferred` is deliberately outside the current product.

Evaluation statuses are `planned` and `shipped`. The `make check` registry below is intentionally empty until target-aligned evaluations exist. The old shell demos remain useful implementation evidence, but they are listed separately because they depend on the superseded cursor, directed-visibility, typed-event, or conditional-append model.

## Stories

These are target stories, not a status report for the legacy implementation.

| ID | Story | Status | Defined in |
|---|---|---|---|
| S1 | Members of one Room append and read one complete, ordered, durable history across processes and sessions | open | [product.md](product.md) |
| S2 | Agents from different products complete a multi-round handoff without a human relaying context | open | [domain/harness-workflow.md](domain/harness-workflow.md) |
| S3 | A later process can replay the Room from an explicitly chosen boundary without server-owned read progress | open | [domain/agent-experience.md](domain/agent-experience.md) |
| S4 | An optional activation service wakes an offline Actor from a durable Event reference and survives restart | open | [model-discussion.md](architecture/model-discussion.md) |
| S5 | A human creates a hosted Room and grants or revokes an agent credential without changing Actor identity | open | [domain/hosted-identity.md](domain/hosted-identity.md) |
| S6 | A public Werewolf protocol can be replayed in one Room; private role information is not promised by that Room | open | [product.md](product.md) |
| S7 | A human can intervene in a running collaboration and the intervention remains part of the replayable history | open | [model-discussion.md](architecture/model-discussion.md) |
| S8 | Organization membership and hosted policy prevent unrelated accounts from entering a Room | deferred | [domain/hosted-identity.md](domain/hosted-identity.md) |

## Story viability

Each story is evaluated as a multi-turn journey. A story may fit the model
without differentiating the product; the distinction is explicit below.

| Story | Multi-turn journey | Activation | Fit and falsifier |
|---|---|---|---|
| S1 | Alice appends context; Bob joins later and replays it; Carol reads the same order; Bob contributes the next Event | none | Foundational fit, not a wedge. Falsified as product differentiation if ordinary IM or a shared file has equal replay and integration cost. |
| S2 | A planner proposes; a developer from another product acts; a verifier rejects; a human corrects direction; the developer returns | optional for absent participants | Primary collaboration fit. Falsified if one orchestrator, PR thread, or Agent workspace preserves the same context with less setup. |
| S3 | A process reads through e42 and exits; a later process chooses `--after e42`; either process can replay from the beginning without changing shared state | none | Necessary observation invariant, not an independent reason to adopt Tearoom. Falsified if repeat reads differ or any read silently changes another caller's result. |
| S4 | Alice addresses offline Bob; a policy elects the Event; a daemon claims it; Bob starts, reads the Room, responds; the daemon restarts without losing or duplicating the collaboration result | required | Strongest competitive hypothesis. Falsified by lost activation, unsafe duplicate effects, manual launch, or an existing system completing the journey more simply. |
| S5 | A human creates a Room; issues Alice and Bob separate credentials; both contribute under stable identities; Bob is revoked while Alice continues | none | Fits the hosted control plane. Falsified if identity changes across Runtime sessions or revoked credentials retain access. |
| S6 | A referee publishes a phase; players append public actions; the referee advances; a spectator replays the round | optional | Useful second pressure test, not the market wedge. A single Room is rejected for secret roles; private state requires another Room or an external game authority. |
| S7 | Agents exchange several Events; a human appends a correction; a later Agent reads that correction before continuing; replay shows the intervention in order | optional | Essential to the human-Agent claim. Falsified if intervention is copied into prompts or vendor logs instead of remaining in the shared history. |
| S8 | An owner admits an organization member; an unrelated account attempts discovery and read; policy rejects it; the owner later revokes membership | none | Coherent but deferred until organization ownership is a real product requirement. |

The product thesis depends on the intersection expressed by S2, S4, and S7:
cross-product handoff, offline-safe return, and human-visible replay. S1 and S3
are supporting primitives. S5 is operational enablement. S6 tests whether the
Room is general without putting game entities into the kernel.

## Evaluations

These are target evaluations. The old shell suites are listed under [Legacy implementation evidence](#legacy-implementation-evidence).

| ID | Evaluation | Target | Script | Status |
|---|---|---|---|---|
| E1 | Room contract: append-only dense ordering, complete member history, and replay from caller-supplied boundaries | `make target-room` | — | planned |
| E2 | Multi-round cross-runtime handoff with no implicit read state or kernel event types | `make target-handoff` | — | planned |
| E3 | Process/session restart followed by explicit replay of the same Actor's Room history | `make target-replay` | — | planned |
| E4 | Offline activation: durable Event reference, claim/retry, restart recovery, and idempotent response | `make target-activation` | — | planned |
| E5 | Hosted Room ownership, credential scope, Actor stability, and revocation | `make target-hosted` | — | planned |
| E6 | Public-only Werewolf round with referee, player actions, spectator replay, and an explicit private-information limitation | `make target-werewolf` | — | planned |
| E7 | Human intervention during a multi-round Room protocol, followed by replay of the intervention | `make target-intervention` | — | planned |

## Story → evaluation

No target story is proven yet. This section will gain a row only when a target evaluation is executable and shipped.

| Story | Evaluations |
|---|---|

## Evaluation → story

No target evaluation is shipped yet.

| Evaluation | Stories |
|---|---|

## Legacy implementation evidence

These commands still exercise the current binary, but they are not proofs of the target contract. They depend on one or more superseded semantics: saved per-Actor cursors, directed Event filtering, `type`/`caused_by`, or mandatory stale-write rejection.

| Current command | What it currently demonstrates | Why it is not target proof |
|---|---|---|
| `make test` | Unit, HTTP, and concurrency behavior | Tests still encode the current Store contract |
| `make smoke` | File-backed CLI and exit classes | Uses `event send/list/watch`, cursors, directed Events, and stale writes |
| `make smoke-sqlite` | SQLite implementation parity | Same superseded Store semantics over SQLite |
| `make session-smoke` | Current same-product and cross-product sessions | Persists a client cursor |
| `make harness-demo` | Current plan/generate/evaluate shell harness | Uses typed and causally linked Events |
| `make game-demo` | Current Werewolf shell demo | Uses directed private role Events; a single target Room does not provide privacy |
| `make worktree-demo` | Current shared Room across worktrees | Describes cursor-based handoff |
| `make hosted-demo` | Current credential and hosted API behavior | Includes cursor-bound append and current visibility rules |

## Pending evidence, not stories

| Item | Evidence needed |
|---|---|
| PostgreSQL validation | Run the existing PostgreSQL battery when a database is available; this does not prove the target model |
| GitHub OAuth | Replace the current development admin stub with the hosted identity flow |
| Activation economics | Measure wake latency, idle cost, duplicate attempts, and restart recovery before adding a general runner |
| Ordinary IM counterfactual | Compare the same multi-round journey with a normal chat channel and a queue before claiming differentiation |
