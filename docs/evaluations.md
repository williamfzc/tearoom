---
type: evaluations
title: Evaluation set
description: The single user story, its staged proof, and supporting deployment evidence.
tags: [evaluations, stories, testing, registry]
timestamp: 2026-09-09
---

# Evaluation set

This is the proof registry for [product.md](product.md). A story is `open`
while any mapped evaluation is planned and `proven` only when every mapped
evaluation ships. Evaluation status is `planned` or `shipped`.

Tearoom has one product story. Storage, restart, remote deployment,
authorization, human participation, and alternate domains are requirements or
test conditions rather than additional stories.

## Staged proof

### P0 — Foundation

Evaluations E1 and E2 are shipped and run in `make check`.

They prove the mechanics needed by the story:

1. Multiple Actors append to one Room and every member reads the same complete,
   dense, ordered history.
2. A caller reads from the beginning or an explicit `after` boundary without
   changing Room state or another caller's result.
3. A later process replays the same history after the earlier process and
   service have stopped.
4. Concurrent append and idempotent retry preserve one accepted order.
5. `to` remains public addressing and never hides an Event.

P0 does not prove that Tearoom is more useful than a shared file, chat, or
vendor transcript. That requires the full cross-product journey.

### P1 — One cross-product continuation story

Story S1. Evaluation E3. **Status: open.**

The journey must use at least two independent Agent products and no human may
copy Event content between them:

```bash
# Owner setup, then keep the server running in this Room.
tearoom room init --store sqlite --as agent-a
tearoom room join agent-b
tearoom room join agent-c
tearoom server start

# Agent product A writes.
TEAROOM_SERVER=http://127.0.0.1:8787 TEAROOM_ACTOR=agent-a \
  tearoom send --json '{"work":"first attempt"}'

# A later process in agent product B pulls, continues, and writes back.
TEAROOM_SERVER=http://127.0.0.1:8787 TEAROOM_ACTOR=agent-b \
  tearoom read --after e0
TEAROOM_SERVER=http://127.0.0.1:8787 TEAROOM_ACTOR=agent-b \
  tearoom send --json '{"result":"second attempt"}'

# Product A or a third product pulls the new result and can continue again.
TEAROOM_SERVER=http://127.0.0.1:8787 TEAROOM_ACTOR=agent-c \
  tearoom read --after e1
```

The exact command, JSON, JSONL, and failure behavior is owned by
[reference/cli.md](reference/cli.md). A human correction uses the same
`room join`, `read`, and `send` path and is not a separate evaluation.

Go only when B recovers A's context, B's result is durable, and A or C can
continue without hidden vendor state or manual relay. No-go if a shared file,
normal chat, repository document, or one product's native transcript completes
the same journey with less setup and equal recovery.

### P2 — Remote readiness

Evaluation E7. **Status: planned.**

Run the same P1 journey against the shared remote service:

1. The service uses PostgreSQL and HTTPS.
2. Each Agent has a stable identity and separate scoped credential.
3. Agents still use ordinary `read` and `send`; only connection and identity
   setup differ.
4. Revoking one credential blocks that credential without changing prior
   Events, the Room, or other Actors.
5. Commands, Event shapes, ordering, read boundaries, retry behavior, and error
   classes match the local profile.

Remote readiness is a deployment gate, not another user story.

## Stories

| ID | Story | Status | Stage | Defined in |
|---|---|---|---|---|
| S1 | When work moves between people, processes, or Agent products, the receiver pulls the existing Room history, continues, and writes back so another participant can continue again without manual context relay | open | P1 | [product.md](product.md) |

## Evaluations

| ID | Evaluation | Target | Script | Status |
|---|---|---|---|---|
| E1 | Foundation Room contract across file, SQLite, and loopback HTTP | `make target-room` | [target-room.sh](../scripts/target-room.sh) | shipped |
| E2 | Foundation replay across real processes and service restarts | `make target-replay` | [target-replay.sh](../scripts/target-replay.sh) | shipped |
| E3 | The four-step cross-product CLI loop: A writes, B pulls, B writes, A or C pulls | `make target-handoff` | — | planned |
| E7 | The same four-step loop on PostgreSQL with remote identity, credentials, and revocation | `make target-remote` | — | planned |

## Story → evaluation

| Story | Evaluations |
|---|---|
| S1 | E1, E2, E3 |

## Evaluation → story

| Evaluation | Stories |
|---|---|
| E1 | S1 |
| E2 | S1 |
| E3 | S1 |

E7 proves deployment readiness rather than a separate user outcome, so it is
not part of the story mapping.

## Hand-run evidence

Evidence that cannot run in `make check` is recorded here or it never ran.

| Date | Evaluation | Cases | Outcome | Record |
|---|---|---|---|---|
| 2026-09-02 | Foundation rival | Incremental reads, per-participant boundaries, replay, activation, and concurrent append compared with a plain JSONL file | The file matched the foundation claims; the cross-product continuation story remains unproven | [lessons/rival-falsification.md](lessons/rival-falsification.md) |

## Legacy surfaces at the P0 cutover

The pre-cutover suites proved a rejected contract and were deleted. E1 and E2
are the only shipped CLI-level proofs. The removed harness demo does not prove
E3 because it used the old Event and cursor model. Old game, worktree, hosted,
session, and smoke demos remain historical evidence in [log.md](log.md), not
current evaluations.

## Pending evidence, not stories

| Item | Evidence needed |
|---|---|
| Cross-product continuation | Run E3 with two independent Agent products and record setup effort, manual relay count, recovery, and repeated handoff |
| Cheapest-rival comparison | Run the same E3 journey through a shared JSONL file, normal chat, repository document, and one vendor transcript |
| PostgreSQL validation | Run the Store battery and E7 when a database and remote service are available |
