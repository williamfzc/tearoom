---
type: evaluations
title: Evaluation set
description: Staged target stories, gates, and evidence for the Room product.
tags: [evaluations, stories, testing, registry]
timestamp: 2026-08-30
---

# Evaluation set

This registry is the target contract in [model-discussion.md](architecture/model-discussion.md). `open` means the target story has not passed its evaluation; `proven` is reserved for an executable target-aligned evaluation; `deferred` is outside the current product. Evaluation statuses are `planned` and `shipped`.

The product advances through four gates. A later gate cannot repair a failed earlier contract. Activation is optional: failure at its gate leaves the Room and handoff product valid, but removes the claim that Tearoom can wake offline Agents.

## Staged gates

### P0 — Minimum Room contract

Stories: S1, S3. Evaluations: E1, E2. **Status: passed at the P0 clean break (2026-08-30)** — [target-room.sh](../scripts/target-room.sh) and [target-replay.sh](../scripts/target-replay.sh) ship with the release and run in `make check`.

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

Go/no-go: go only when local and remote implementations pass the same assertions. No-go if either implementation still needs a Room-owned read cursor, private Event filtering, or mandatory stale-write preconditions to function. Both shipped Stores (file and relational/SQLite) pass the core contract battery; the PostgreSQL battery is gated behind `TEAROOM_TEST_PG` and skips when no database is configured.

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
| S1 | Members of one Room append and read one complete, ordered, durable history across processes and sessions | proven | P0 | [product.md](product.md) |
| S2 | Agents from different products complete a multi-round handoff without a human relaying context | open | P1 | [domain/harness-workflow.md](domain/harness-workflow.md) |
| S3 | A later process can replay the Room from an explicitly chosen boundary without server-owned read progress | proven | P0 | [domain/agent-experience.md](domain/agent-experience.md) |
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
| E1 | P0 Room contract: dense append order, complete member history, and pure reads from explicit boundaries | `make target-room` | [target-room.sh](../scripts/target-room.sh) | shipped |
| E2 | P0 process/session replay without server-owned read progress | `make target-replay` | [target-replay.sh](../scripts/target-replay.sh) | shipped |
| E3 | P1 three-round cross-product handoff without kernel event types or implicit read state | `make target-handoff` | — | planned |
| E4 | P1 human intervention followed by ordered replay and continuation | `make target-intervention` | — | planned |
| E5 | P2 optional offline activation, durable claim, restart recovery, and duplicate-safe response | `make target-activation` | — | planned |
| E6 | P3 public-only Werewolf round with referee, players, and spectator replay | `make target-werewolf` | — | planned |
| E7 | P3 hosted credentials, stable Actors, revocation, and membership hardening | `make target-hosted` | — | planned |

## Story → evaluation

| Story | Evaluations |
|---|---|
| S1 | E1 |
| S3 | E2 |

## Evaluation → story

| Evaluation | Stories |
|---|---|
| E1 | S1 |
| E2 | S3 |

## Legacy surfaces at the P0 cutover

The pre-cutover suites were a second contract, not a target proof. At the
clean break they were deleted from `make check` and from the tree; the shipped
target evaluations [target-room.sh](../scripts/target-room.sh) and
[target-replay.sh](../scripts/target-replay.sh) are the only CLI-level proofs.

| Removed surface | Migration outcome |
|---|---|
| `make smoke` / `smoke.sh` | Deleted; `event` nesting, saved cursors, directed privacy, `type`/`caused_by`, and mandatory stale writes are gone |
| `make smoke-sqlite` / `smoke-sqlite.sh` | Deleted; SQLite coverage lives in the relational store contract battery |
| `make session-smoke` / `session-smoke.sh` | Deleted; replay is E2 |
| `make harness-demo` / `harness-demo.sh` | Deleted; E3/E4 remain planned |
| `make game-demo` / `game-demo.sh` | Deleted; E6 remains planned |
| `make worktree-demo` / `worktree-demo.sh` | Deleted; explicit-boundary replay is E2 |
| `make hosted-demo` / `hosted-demo.sh` | Deleted with the hosted implementation; E7 remains planned after the future hosted phase |
| `make pg-smoke` / `pg-smoke.sh` | Deleted; PostgreSQL validation is the gated `TEAROOM_TEST_PG` store battery |

A green `make check` therefore proves the target P0 contract, not the old
implementation.

## Pending evidence, not stories

| Item | Evidence needed |
|---|---|
| PostgreSQL validation | Run the target Store battery when a database is available; backend parity does not prove product differentiation |
| Activation economics | Measure wake latency, idle cost, duplicate attempts, and restart recovery before adding a general runner |
| Ordinary IM counterfactual | Compare the same P1 journey with a normal chat channel, shared workspace, and queue |
