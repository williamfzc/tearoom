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

The acceptance journeys are defined in [domain/user-stories.md](domain/user-stories.md).
Storage, concurrency, and restart behavior are requirements and test
conditions rather than additional stories. The hc-backed collaboration
journeys remain unproven.

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

The stories need real Agent runs and cannot run in `make check`; the hand-run
method is recorded under [Hand-run evidence](#hand-run-evidence). Each journey
is open until it has run against the Room message model and its evidence row
exists. No current evidence proves the new hc-backed collaboration journeys.

## Stories

| ID | Story | Status | Stage | Defined in |
|---|---|---|---|---|
| S1 | One Agent completes one task | open | acceptance | [user-stories.md](domain/user-stories.md#s1-one-agent-completes-one-task) |
| S2 | Builder and independent reviewer share a project Room | open | acceptance | [user-stories.md](domain/user-stories.md#s2-builder-and-reviewer-share-a-project-room) |
| S3 | A new process resumes work later from Room history | open | acceptance | [user-stories.md](domain/user-stories.md#s3-a-new-process-resumes-later) |
| S4 | A mention reaches its intended collaborator through hc | open | acceptance | [user-stories.md](domain/user-stories.md#s4-a-message-reaches-its-intended-collaborator) |
| S5 | An Agent discovers collaborators and chooses whom to mention | open | acceptance | [user-stories.md](domain/user-stories.md#s5-an-agent-discovers-who-can-help) |
| S6 | Shared Room conversation supports broadcast visibility and targeted wakes | open | acceptance | [user-stories.md](domain/user-stories.md#s6-a-room-behaves-like-a-group-conversation) |
| S7 | Builder and reviewer use mentions for review and revision in one Room | open | acceptance | [user-stories.md](domain/user-stories.md#s7-review-and-revision-continue-in-one-room) |
| S8 | Pending delivery recovers after runtime interruption | open | acceptance | [user-stories.md](domain/user-stories.md#s8-pending-work-survives-runtime-interruption) |
| S9 | Agents in separate Rooms coordinate through their common parent Room | open | acceptance | [user-stories.md](domain/user-stories.md#s9-separate-rooms-coordinate-through-their-common-parent) |
| S10 | Reusable role definitions can be configured in Rooms with separate hc panels | open | acceptance | [user-stories.md](domain/user-stories.md#s10-reuse-role-definitions-across-rooms) |
| S11 | An Agent chooses a configured role and suitable Agent type | open | acceptance | [user-stories.md](domain/user-stories.md#s11-choose-a-collaborator-by-role-and-agent-type) |

## Evaluations

| ID | Evaluation | Target | Script | Status |
|---|---|---|---|---|
| E1 | Foundation room-tree contract through the shipped binary: init with parents, per-room dense sequences, concurrency, idempotency, tail, subtree, tree, missing signature, and the HTTP leg | `make target-room` | [target-room.sh](../scripts/target-room.sh) | shipped |
| E2 | Replay across real processes and service restarts with no stored read progress | `make target-replay` | [target-replay.sh](../scripts/target-replay.sh) | shipped |
| E3 | The original journeys S1–S3 with real, independent Agent products | hand-run | — | planned |
| E4 | Room message delivery and explicit mention through hc, including a worker started when the target Agent is not already running | hand-run | — | planned |
| E5 | Reuse role definitions across Rooms, configure roles, inspect role-to-agent assignments, and keep each Room-role on its own hc panel | hand-run | — | planned |
| E6 | End-to-end builder/reviewer collaboration in one Room and branch coordination through a common parent Room | hand-run | — | planned |
| E7 | Recovery after stopping the runtime at delivery and execution boundaries; reconcile uncertain work before retrying | hand-run | — | planned |
| E8 | Agent discovery across multiple Agent types and explicit selection of a configured Room role | hand-run | — | planned |

## Story → evaluation

| Story | Evaluations |
|---|---|
| S1 | E1, E2, E3 |
| S2 | E1, E3, E4, E6 |
| S3 | E2, E3 |
| S4 | E4 |
| S5 | E5, E8 |
| S6 | E4 |
| S7 | E4, E6 |
| S8 | E7 |
| S9 | E6 |
| S10 | E5 |
| S11 | E5, E8 |

## Evaluation → story

| Evaluation | Stories |
|---|---|
| E1 | S1, S2 |
| E2 | S1, S3 |
| E3 | S1, S2, S3 |
| E4 | S2, S4, S6, S7 |
| E5 | S5, S10, S11 |
| E6 | S2, S7, S9 |
| E7 | S8 |
| E8 | S5, S11 |

## Hand-run evidence

Evidence that cannot run in `make check` is recorded here or it never ran.

### Hand-running the acceptance journeys

The acceptance claim needs real, independent Agent products. The recorded
runs follow this method:

1. Setup only: a human or an Agent initializes the project Room and any child
   Rooms needed for S9. Configure the chosen reusable roles and their Agent
   types through the intended CLI. Each Room-role must have its own hc panel.
   Do not place record content in an Agent's launch prompt; prompts are the
   audit trail for the relay count.
2. Start the builder and reviewer as distinct roles in the same project Room.
   The reviewer reads the shared Room history, is explicitly mentioned, and
   writes its verdict and evidence in that same Room. A requested revision is
   addressed to the builder there. For S9, use two child Rooms and their common
   parent as the shared coordination Room; do not write directly across
   sibling Rooms.
3. For S10 and S11, reuse a role definition in more than one Room, configure
   multiple Agent types, then ask an Agent to inspect the configured roles and
   select a target. Verify each Room-role uses an independent hc panel.
4. S1 runs one Agent to completion with zero interventions. S3 starts a fresh
   process a day later, optionally with a different tool, and asks the four
   resume questions. S4, S6, and S8 exercise online and offline mentions,
   broadcasts, and runtime interruption/reconciliation.
5. Record setup effort, human relay count, Room and panel mapping, delivery
   observations, recovery quality, and verdict evidence in the table below.

A scripted stand-in for an Agent or runtime is not acceptance evidence. For
S4–S11, record the Room messages, configured role list, role-to-Agent-type
mapping, hc panel identities and lifecycle events, prompts, and final Room
records needed to reconstruct routing and recovery. Include a run where a
target is offline and a run where the runtime restarts with work pending. The
proposed broadcast policy is that ordinary messages remain visible but do not
start every offline Agent; explicit mentions reach the named role. Role
configuration is routing context, not a permission or identity-verification
boundary. The 2026-11-07 file-based wake feasibility row below is background
only and does not prove this hc-backed design.

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
