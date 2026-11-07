---
type: evaluations
title: Evaluation set
description: The acceptance journeys, their staged proof, and supporting hand-run evidence.
tags: [evaluations, stories, testing, registry]
timestamp: 2026-11-07
---

# Evaluation set

This is the proof registry for [product.md](product.md). A story is `open`
while any mapped evaluation is planned and `proven` only when every mapped
evaluation ships. Evaluation status is `planned` or `shipped`.

The product is proven by three acceptance journeys — solo delivery,
build-and-review, and resume across a day — plus the wake journey through the
notification envelope. Storage, concurrency, and restart behavior are
requirements and test conditions rather than additional stories.

## Staged proof

### Foundation

Evaluations E1 and E2 are shipped and run in `make check`. They prove the
mechanics the journeys rely on:

1. Rooms form a tree: children record their parent, child lists are derived
   at read time, and each Room's stream is independent and dense.
2. Multiple writers append concurrently and the Store issues one dense
   sequence per Room; an idempotent retry returns the original record.
3. A caller reads from the beginning, an explicit `after` boundary, a tail,
   or a subtree without changing Room state.
4. A later process replays the same history after the earlier process and
   service have stopped; no session or cursor files exist anywhere.

The foundation does not prove that a real Agent can work from the tree alone.
That requires the acceptance journeys.

### The acceptance journeys

S1–S3 are the completion standard of [product.md](product.md); S4 is the
notification story. They need real, independent Agent products and cannot run
in `make check`; the hand-run method is recorded under
[Hand-run evidence](#hand-run-evidence). Each journey is open until it has
run against the room-tree model and its evidence row exists.

## Stories

| ID | Story | Status | Stage | Defined in |
|---|---|---|---|---|
| S1 | One Agent completes a task end to end with zero human intervention, reading only its Room | open | acceptance | [product.md](product.md) |
| S2 | Two Agents work as builder and reviewer; the reviewer reads only the implementation Room and returns pass or fail with evidence, and the executor never certifies its own work | open | acceptance | [product.md](product.md) |
| S3 | A new process, optionally a new tool, a day later, reads only the Rooms and answers the goal, the prior work, whether acceptance is met, and what is next | open | acceptance | [product.md](product.md) |
| S4 | A record append reaches an external waker through the notification envelope and starts the next worker with no human relay | open | acceptance | [product.md](product.md) |

## Evaluations

| ID | Evaluation | Target | Script | Status |
|---|---|---|---|---|
| E1 | Foundation room-tree contract through the shipped binary: init with parents, per-room dense sequences, concurrency, idempotency, tail, subtree, tree, missing signature, and the HTTP leg | `make target-room` | [target-room.sh](../scripts/target-room.sh) | shipped |
| E2 | Replay across real processes and service restarts with no stored read progress | `make target-replay` | [target-replay.sh](../scripts/target-replay.sh) | shipped |
| E3 | The acceptance journeys S1–S3 with two real, independent Agent products | hand-run | — | planned |
| E4 | The wake journey S4: envelope from `watch --json` to a herdr-started worker | hand-run | — | planned |

## Story → evaluation

| Story | Evaluations |
|---|---|
| S1 | E1, E2, E3 |
| S2 | E1, E3 |
| S3 | E2, E3 |
| S4 | E4 |

## Evaluation → story

| Evaluation | Stories |
|---|---|
| E1 | S1, S2 |
| E2 | S1, S3 |
| E3 | S1, S2, S3 |
| E4 | S4 |

## Hand-run evidence

Evidence that cannot run in `make check` is recorded here or it never ran.

### Hand-running the acceptance journeys

The acceptance claim needs real, independent Agent products. The recorded
runs follow this method:

1. Setup only: a human or an Agent at the root runs `room init` (and, for
   S2, creates the child Rooms). No record content may appear in any prompt;
   the prompts are the audit trail for the relay count.
2. Each Agent product runs in its own directory and receives only the binary
   path, the workspace location, the room names, and its signature. Recovery
   must come from reading the Rooms.
3. S1 runs one Agent to completion with zero interventions. S2 has a second
   Agent review while reading only the implementation Room; its verdict must
   cite commands and output that exist only in the records. S3 starts a fresh
   process a day later, optionally with a different tool, and asks the four
   resume questions.
4. Record setup effort, manual relay count, recovery quality, and the
   verdict trail in the table below.

A scripted stand-in for the second product collapses the proof into E1/E2
and is not acceptance evidence.

| Date | Evaluation | Cases | Outcome | Record |
|---|---|---|---|---|
| 2026-11-07 | PostgreSQL battery on the format-3 tree schema | `TEAROOM_TEST_PG` against a temporary local PostgreSQL cluster: the conformance battery over the room tree (named rooms under parents, per-room dense sequences, concurrent append, concurrent idempotency, read boundaries, content compaction, missing signatures) with each subtest isolated by a name prefix in the shared database | All green on the format-3 SQLite/PostgreSQL schema; the SQL adapters were removed the same day by the JSONL cutover, so this row is the closing validation of that era | Executable against the format-3 code (`archive/v1` descendants); the adapter no longer ships |
| 2026-11-07 | E4 wake chain feasibility (v1 file stand-in for the Room) | herdr 0.9.5 with codex 0.159.2 in an isolated session: (1) start an Agent that reads the room, builds and verifies a script, writes the conclusion back; (2) a second prompt continues from the prior record; (3) a brand-new workspace, a fresh Agent given only the room path answers goal, prior work, acceptance, next step; (4) an external append is detected by a polling script that drives `herdr agent prompt` and the Agent wakes, reads, and writes back (~18 s) | All four experiments passed; the wake chain — start, prompt, wait — is reliable, and a process with no history recovered the full state from the room alone. Preconditions recorded: each tool's one-time trust dialogs and login must be pre-cleared, and the local `claude` binary authenticates through the LLMBox wrapper | Design-note experiments 1–4; the waker contract is `tearoom watch --json` plus an external script |
| 2026-09-10 | Live hosted journey (v1 contract) | Render web service `redacted.invalid` over Neon PostgreSQL 18: owner-created room, per-Actor credentials, the four-step continuation with token-free open reads, and the revocation matrix | Four steps closed over the public internet with zero manual relays under the v1 membership model; superseded by the 2026-11-07 convergence | [log.md](log.md) deployment record |
| 2026-09-10 | PostgreSQL validation (gated battery) | `TEAROOM_TEST_PG` against local PostgreSQL 16.15: the conformance battery over the v1 schema plus credential control | All green on the v1 schema; the battery must be rerun against the format-3 tree schema | Executable: `go test ./internal/relational/ -run TestPostgreSQL` with the env set |
| 2026-09-09 | Cross-product continuation (v1 contract) | ZCode as `agent-a` and headless Claude Code as `agent-b` over loopback HTTP with explicit joins: A appended a work contract; B recovered it with `read`, implemented and tested, appended the result; the server restarted; A pulled the result from an explicit boundary and accepted | Four steps closed with zero manual relays and survived a restart under the v1 model | Method above, adapted to the v1 join flow |
| 2026-09-02 | Foundation rival (v1 contract) | Incremental reads, per-participant boundaries, replay, activation, and concurrent append compared with a plain JSONL file | The file matched the v1 foundation claims; the cross-product continuation story remained unproven | [lessons/rival-falsification.md](lessons/rival-falsification.md) |

## Pending evidence, not stories

| Item | Evidence needed |
|---|---|
| Cheapest-rival comparison | Run the same acceptance journeys through a shared JSONL file, normal chat, repository document, and one vendor transcript |
