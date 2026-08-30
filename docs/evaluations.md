---
type: evaluations
title: Evaluation set
description: Staged target stories, gates, and evidence for the Room product.
tags: [evaluations, stories, testing, registry]
timestamp: 2026-08-30
---

# Evaluation set

This registry is the target contract in [model-discussion.md](architecture/model-discussion.md), not a claim about the current binary. `open` means the target story has not passed its evaluation; `proven` is reserved for an executable target-aligned evaluation; `deferred` is outside the current product. Evaluation statuses are `planned` and `shipped`.

The product advances through four gates. A later gate cannot repair a failed earlier contract. Activation is optional: failure at its gate leaves the Room and handoff product valid, but removes the claim that Tearoom can wake offline Agents.

## Staged gates

### P0 — Minimum Room contract

Stories: S1, S3. Evaluations: E1, E2.

Entry conditions:

- The clean-break target model is accepted as authoritative.
- Room, Actor, and Event are the only kernel entities.
- The target Event surface is ordered position, author, time, optional public `to`, and content.
- No target test or command depends on saved cursors, private directed Events, mandatory `type`/`caused_by`, or conditional stale writes.

Executable acceptance:

1. Multiple Actors append to one Room and all members can read one complete dense ordered history.
2. A caller reads from the beginning or an explicit `after` boundary; repeating a read does not mutate Room state or another caller's result.
3. A process ends and a later process replays the same history from a chosen boundary.
4. `to`, when present, is visible addressing metadata and does not hide content from Room members.

Go/no-go: go only when local and remote implementations pass the same assertions. No-go if either implementation still needs a Room-owned read cursor, private Event filtering, or mandatory stale-write preconditions to function.

### P1 — Cross-product handoff and human intervention

Stories: S2, S7. Evaluations: E3, E4.

Entry conditions: P0 passes, and two independent agent products can call the target append/read contract.

Executable acceptance:

1. A planner, implementer, and verifier complete at least three rounds in one Room without a human copying context between products.
2. One process stops between rounds; a later process reads the complete history and continues from a caller-selected boundary.
3. A human appends a correction during the loop; a later Actor reads that correction from the same ordered history before continuing.
4. Replay shows every round and the intervention in order; no kernel `type`, `caused_by`, task, or workflow entity is required.

Go/no-go: go when the journey works without an orchestrator-specific transcript or manual relay. No-go if an ordinary shared workspace, PR thread, or IM channel gives the same recovery and intervention behavior with less setup.

### P2 — Optional offline activation

Story: S4. Evaluation: E5.

Entry conditions: P1 passes; a concrete offline-Actor journey justifies process
wake; and the launcher, policy, retry, fencing, and duplicate-handling contracts
in [activation.md](architecture/activation.md) are accepted.

Executable acceptance:

1. Bob is a Room member with no running process.
2. An allowed Event addressed to Bob creates durable activation work without copying the Event body into a command.
3. A daemon claims the work, starts or resumes Bob, and passes an Event reference; Bob reads the Room and appends a response.
4. Killing and restarting the daemon preserves the opportunity, and retries do not create duplicate collaboration Events.
5. An Event ignored by policy does not start a process.
6. A retryable failure succeeds within its bound, while an exhausted retry
   budget produces visible `dead` operational state instead of an infinite loop
   or a false collaboration result.

Go/no-go: go only if activation has bounded at-least-once attempts, is
restart-safe, and is materially better than manual launch or ordinary polling
at measured cost. No-go means keep activation out of the product claim; it must
not invalidate P0 or P1, and it must not turn the Room into a general scheduler
or queue.

### P3 — Second-domain stress test and hosted hardening

Stories: S5, S6, S8. Evaluations: E6, E7.

Entry conditions: P1 passes. P2 is required only if the chosen second-domain journey depends on offline wake; otherwise it remains optional. Hosted hardening must have a real owner and membership requirement.

Executable acceptance:

1. A public-only Werewolf round uses one Room for referee, player actions, and spectator replay. The evaluation must state that secret roles are impossible in one Room; private state requires another Room or an external authority. This is a stress test, not the market thesis.
2. Hosted identity creates a Room, issues separate scoped credentials to stable Actors, and revokes one without changing the Room or other Actors.
3. Organization membership rejects an unrelated account from discovery and read, if and only if organization ownership is in scope.

Go/no-go: go when the second domain works without adding game entities or hidden privacy semantics to the kernel, and hosted controls preserve stable Actor identity. No-go if game fairness, secret state, money, or organization policy requires a separate authoritative server; keep that capability outside Tearoom.

## Stories

| ID | Story | Status | Stage | Defined in |
|---|---|---|---|---|
| S1 | Members of one Room append and read one complete, ordered, durable history across processes and sessions | open | P0 | [product.md](product.md) |
| S2 | Agents from different products complete a multi-round handoff without a human relaying context | open | P1 | [domain/harness-workflow.md](domain/harness-workflow.md) |
| S3 | A later process can replay the Room from an explicitly chosen boundary without server-owned read progress | open | P0 | [domain/agent-experience.md](domain/agent-experience.md) |
| S4 | An optional activation service wakes an offline Actor from a durable Event reference and survives restart | open | P2 | [activation.md](architecture/activation.md) |
| S5 | A human creates a hosted Room and grants or revokes an agent credential without changing Actor identity | open | P3 | [domain/hosted-identity.md](domain/hosted-identity.md) |
| S6 | A public Werewolf protocol can be replayed in one Room; private role information is not promised by that Room | open | P3 | [product.md](product.md) |
| S7 | A human can intervene in a running collaboration and the intervention remains part of the replayable history | open | P1 | [model-discussion.md](architecture/model-discussion.md) |
| S8 | Organization membership and hosted policy prevent unrelated accounts from entering a Room | deferred | P3 | [domain/hosted-identity.md](domain/hosted-identity.md) |

## Story viability

The product thesis is tested by S2 and S7 first, with S1 and S3 as necessary primitives. S4 is the strongest differentiation hypothesis but remains optional and unproven. S5 is hosted enablement. S6 tests generality without making games the market line. S8 remains deferred until organization ownership is real.

| Story | Multi-turn journey | Falsifier |
|---|---|---|
| S1 | Alice appends; Bob joins later; Carol reads the same order; Bob contributes | Ordinary IM or a shared file gives equal replay and integration cost |
| S2 | Planner proposes; another product acts; verifier rejects; the loop revises | One orchestrator, PR thread, or Agent workspace preserves context more simply |
| S3 | One process reads from an explicit boundary; a later process replays from that boundary | Repeated reads differ or silently change another caller's result |
| S4 | Offline Bob is activated, reads the Room, responds, and survives daemon restart | Lost activation, unsafe duplicate effects, manual launch, or excessive idle cost |
| S5 | Separate hosted credentials act as stable Actors; one is revoked | Runtime/session changes identity or revoked credentials retain access |
| S6 | Referee publishes phases; players append public actions; spectator replays | Secret roles or authoritative settlement must be hidden in the single Room |
| S7 | Human correction is appended; a later Agent reads it before continuing | Correction exists only in a prompt, vendor transcript, or private control channel |

## Evaluations

| ID | Evaluation | Target | Script | Status |
|---|---|---|---|---|
| E1 | P0 Room contract: dense append order, complete member history, and pure reads from explicit boundaries | `make target-room` | — | planned |
| E2 | P0 process/session replay without server-owned read progress | `make target-replay` | — | planned |
| E3 | P1 three-round cross-product handoff without kernel event types or implicit read state | `make target-handoff` | — | planned |
| E4 | P1 human intervention followed by ordered replay and continuation | `make target-intervention` | — | planned |
| E5 | P2 optional offline activation, durable claim, restart recovery, and duplicate-safe response | `make target-activation` | — | planned |
| E6 | P3 public-only Werewolf round with referee, players, and spectator replay | `make target-werewolf` | — | planned |
| E7 | P3 hosted credentials, stable Actors, revocation, and membership hardening | `make target-hosted` | — | planned |

## Story → evaluation

No target story is proven yet. A story enters this map only after its target evaluation is executable and shipped.

| Story | Evaluations |
|---|---|

## Evaluation → story

No target evaluation is shipped yet.

| Evaluation | Stories |
|---|---|

## Legacy migration at P0 entry

The current shell suites are not a second contract. When migration starts, remove them from `make check` and either delete them or rewrite them against the target model. Do not retain them indefinitely as “legacy” proofs.

| Current suite | Required migration action |
|---|---|
| `make test` | Rewrite Store assertions for complete member history, pure reads, and ordinary append; delete tests whose only purpose is stale cursor rejection or private filtering |
| `make smoke` | Rewrite as the P0 CLI evaluation; remove `event` nesting, saved cursors, directed privacy, `type`/`caused_by`, and mandatory stale writes |
| `make smoke-sqlite` | Rewrite against the same P0 contract, then keep only if SQLite remains a supported target |
| `make session-smoke` | Rewrite as P0 replay from caller-selected boundaries; otherwise delete |
| `make harness-demo` | Rewrite as E3/E4 with content conventions only; delete the typed/causal version |
| `make game-demo` | Rewrite as E6 public-only Werewolf, or delete if the second-domain gate is not pursued |
| `make worktree-demo` | Rewrite as explicit-boundary replay, or delete |
| `make hosted-demo` | Rewrite as E7 after the target HTTP contract exists, or delete |

Until that migration is done, a green `make check` proves only the old implementation. It must not be reported as proof of any target story.

## Pending evidence, not stories

| Item | Evidence needed |
|---|---|
| PostgreSQL validation | Run the target Store battery when a database is available; backend parity does not prove product differentiation |
| Activation economics | Measure wake latency, idle cost, duplicate attempts, and restart recovery before adding a general runner |
| Ordinary IM counterfactual | Compare the same P1 journey with a normal chat channel, shared workspace, and queue |
