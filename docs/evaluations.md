---
type: evaluations
title: Evaluation set
description: The single user story, its staged proof, and supporting deployment evidence.
tags: [evaluations, stories, testing, registry]
timestamp: 2026-09-10
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

P0 does not prove that Tearoom is more useful than a shared file, chat, or
vendor transcript. That requires the full cross-product journey.

### P1 — One cross-product continuation story

Story S1. Evaluation E3. **Status: open.**

The journey must use at least two independent Agent products and no human may
copy Event content between them:

```bash
# Owner setup, then keep the server running in this Room.
tearoom room init --as agent-a
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

A hand-run instance of this journey and the method that produced it are
recorded under [Hand-run evidence](#hand-run-evidence).

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
| E1 | Foundation Room contract over the local SQLite room and the loopback HTTP service | `make target-room` | [target-room.sh](../scripts/target-room.sh) | shipped |
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

### Hand-running the E3 journey

The cross-product claim of E3 needs two real, independent Agent products and
cannot run in `make check`. The recorded runs follow this method:

1. The owner performs setup only — `room init`, one `room join` per Actor,
   `server start` — and never carries Event content.
2. Each Agent product runs in its own directory and receives only the binary
   path, the Room URL, its Actor identity, and the instruction to recover its
   context from the Room with `read`. No Event content may appear in any
   prompt; the prompts are the audit trail for the relay count.
3. The journey includes a service restart and an explicit `--after` boundary.
   The second participant's result must echo details that exist only in the
   Room; that echo is the recovery proof.
4. Record setup effort, manual relay count, recovery quality, and the repeated
   handoff in the table below.

A scripted stand-in for the second product collapses this proof into E1/E2
and is not E3 evidence.

| Date | Evaluation | Cases | Outcome | Record |
|---|---|---|---|---|
| 2026-09-02 | Foundation rival | Incremental reads, per-participant boundaries, replay, activation, and concurrent append compared with a plain JSONL file | The file matched the foundation claims; the cross-product continuation story remains unproven | [lessons/rival-falsification.md](lessons/rival-falsification.md) |
| 2026-09-10 | PostgreSQL validation (gated battery) | `TEAROOM_TEST_PG` against local PostgreSQL 16.15: the 14-case contract battery (dense concurrent append, concurrent idempotency, membership visibility, read boundaries, content compaction) plus credential issue/verify/revoke on the additive credentials table | All green; PostgreSQL is validated as the remote database adapter, and the battery reruns wherever `TEAROOM_TEST_PG` points | Executable: `go test ./internal/relational/ -run TestPostgreSQL` with the env set |
| 2026-09-09 | E3 journey (hand-run) | ZCode as `agent-a` and headless Claude Code as `agent-b` in its own directory over loopback HTTP. Setup: `room init --store sqlite`, two joins, `server start`. A appended a work contract (e1); B recovered it with `read`, implemented and tested the work, and appended the result (e2) with its own idempotency key; the server was killed and restarted; A pulled e2 with `--after e1` and appended acceptance (e3); C read the full history | Four steps closed with zero manual Event relays; B echoed contract details that existed only in the Room; the dense 1..3 history survived the restart; setup was five commands and B ran unattended in one ~2-minute headless session | [method](#hand-running-the-e3-journey) |

## Legacy surfaces at the P0 cutover

The pre-cutover suites proved a rejected contract and were deleted. E1 and E2
are the only shipped CLI-level proofs. The removed harness demo does not prove
E3 because it used the old Event and cursor model. Old game, worktree, hosted,
session, and smoke demos remain historical evidence in [log.md](log.md), not
current evaluations.

## Pending evidence, not stories

| Item | Evidence needed |
|---|---|
| Cheapest-rival comparison | Run the same E3 journey through a shared JSONL file, normal chat, repository document, and one vendor transcript |
| Live E7 | Deploy the hosted service behind TLS on a real PostgreSQL and record the four-step journey with revocation; the Store battery and credential control already pass on PostgreSQL 16.15 |
