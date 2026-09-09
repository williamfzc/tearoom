---
type: evaluations
title: Evaluation set
description: Staged target stories, gates, and evidence for the Room product.
tags: [evaluations, stories, testing, registry]
timestamp: 2026-09-09
---

# Evaluation set

This registry is the target contract in [product.md](product.md). `open` means the target story has not passed its evaluation; `proven` is reserved for an executable target-aligned evaluation; `deferred` is outside the current product. Evaluation statuses are `planned` and `shipped`.

The product advances through three active gates. Delivery to an absent Agent is
a paste, a context link, or an operator-owned external script; the rejected
in-product wake design is historical evidence in
[lessons/rival-falsification.md](lessons/rival-falsification.md), not a current
gate.

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

Go/no-go: go only when every shipped adapter passes the same assertions. No-go
if any adapter needs a Room-owned read cursor, private Event filtering, or
mandatory stale-write preconditions to function. Both shipped Stores (file and
relational/SQLite) pass the core contract battery. PostgreSQL parity belongs to
P2; its battery is gated behind `TEAROOM_TEST_PG` and skips when no database is
configured.

### P1 — Cross-product handoff and human intervention

Stories: S2, S7. Evaluations: E3, E4.

Entry conditions: P0 passes, and two independent agent products can call the target append/read contract.

Executable acceptance:

1. A planner, implementer, and verifier complete at least three rounds in one Room without a human copying context between products.
2. One process stops between rounds; a later process reads the complete history and continues from a caller-selected boundary.
3. A human appends a correction during the loop; a later Actor reads that correction from the same ordered history before continuing.
4. Replay shows every round and the intervention in order; no kernel `type`, `caused_by`, task, or workflow entity is required.

Go/no-go: go when the journey works without an orchestrator-specific transcript or manual relay. No-go if an ordinary shared workspace, PR thread, or IM channel gives the same recovery and intervention behavior with less setup.

### P2 — Context sharing, remote access, and second-domain stress

Stories: S5, S6, S9. Evaluations: E6, E7, E8. S8 remains outside the current
product until organization ownership is adopted.

Entry conditions: P1 passes. No second-domain journey may depend on an
in-product wake service. Remote hardening extends the same service and must have
a real owner and membership requirement.

Executable acceptance:

1. A context link created from a Room range resolves read-only for a human
   (replay view) and as text for an agent with no Tearoom install.
2. The same service runs locally on SQLite and remotely on PostgreSQL without
   changing its Room/Event contract, commands, errors, migrations, or
   authorization decisions. Remote identity creates a Room, issues separate
   scoped credentials to stable Actors, and revokes one without changing the
   Room or other Actors.
3. A continuation Room created from a snapshot preserves the Room/Actor/Event
   contract; the chat thread carries links and never becomes Event state.
4. A public-only Werewolf round uses one Room for referee, player actions, and
   spectator replay. Secret roles are impossible in one Room; private state
   requires another Room or an external authority. This is a stress test, not
   the market thesis.

Go/no-go: go when the second domain works without adding game entities or
hidden privacy semantics to the kernel, and one service passes the same journey
on SQLite and PostgreSQL while preserving stable Actor identity. No-go if
remote deployment needs a second product contract or if game fairness, secret
state, money, or organization policy requires a separate authoritative server.

## Stories

| ID | Story | Status | Kind | Stage | Defined in |
|---|---|---|---|---|---|
| S1 | Members of one Room append and read one complete, ordered, durable history across processes and sessions | proven | Core story | P0 | [product.md](product.md) |
| S2 | Agents from different products complete a multi-round handoff without a human relaying context | open | Core story | P1 | [domain/harness-workflow.md](domain/harness-workflow.md) |
| S3 | A later process can replay the Room from an explicitly chosen boundary without server-owned read progress | proven | S1 contract scenario | P0 | [domain/concepts.md](domain/concepts.md) |
| S5 | A Room owner creates a remote Room, grants one stable Agent limited access, and revokes it without affecting other participants or history | open | Remote owner story | P2 | [product.md](product.md) |
| S6 | A public Werewolf protocol can be replayed in one Room; private role information is not promised by that Room | open | Stress scenario | P2 | [product.md](product.md) |
| S7 | A human can intervene in a running collaboration and the intervention remains part of the replayable history | open | Core story | P1 | [product.md](product.md) |
| S8 | Organization membership and remote policy prevent unrelated accounts from entering a Room | deferred | Future policy | Future | [product.md](product.md) |
| S9 | A context link shared in a chat thread resolves as a human replay and as agent-readable text, and a continuation appends under the same Room contract | open | Distribution story | P2 | [product.md](product.md) |

S3 remains registered for traceability because E2 already proves it, but it is
an acceptance scenario inside S1 rather than a separate product outcome. S6 is
a generality test, not a market story. S8 is not part of an active gate.

## Expected CLI journeys

The command grammar, output shapes, environment variables, and failure classes
below are owned by the [CLI reference](reference/cli.md). This section composes
that interface into the user-visible journeys each story must pass. Commands
under the target contract are explicit expectations but do not claim current
implementation.

### S1 and S3 — shared history and later replay

```bash
tearoom room init --store sqlite --as alice
tearoom room join bob
tearoom send "first handoff" --as alice
tearoom send --json '{"result":"reviewed"}' --as bob --idempotency-key review-1
tearoom read --as alice
tearoom read --as bob --after e1
```

The first command returns the Room, the join returns Bob, each send returns the
accepted Event, and each read emits JSONL. Both members see the same complete
order; the final read starts at e2. Running that final command in a later
process gives the same result and changes no stored read position. E1 and E2
ship this journey for the current file, SQLite, and loopback profiles.

### S2 — cross-product handoff

The owner initializes a Room, admits the three stable Actors, and starts the
loopback service:

```bash
tearoom room init --store sqlite --as planner
tearoom room join implementer
tearoom room join verifier
tearoom server start
```

Three independent product processes point at the same Room. Each reads before
it appends, and passes its last observed Event address explicitly:

```bash
TEAROOM_SERVER=http://127.0.0.1:8787 TEAROOM_ACTOR=planner tearoom send --json '{"plan":"v1"}'
TEAROOM_SERVER=http://127.0.0.1:8787 TEAROOM_ACTOR=implementer tearoom read --after e0
TEAROOM_SERVER=http://127.0.0.1:8787 TEAROOM_ACTOR=implementer tearoom send --json '{"implementation":"v1"}'
TEAROOM_SERVER=http://127.0.0.1:8787 TEAROOM_ACTOR=verifier tearoom read --after e1
TEAROOM_SERVER=http://127.0.0.1:8787 TEAROOM_ACTOR=verifier tearoom send --json '{"verdict":"revise"}'
TEAROOM_SERVER=http://127.0.0.1:8787 TEAROOM_ACTOR=planner tearoom read --after e2
```

The journey passes only when the products are independent, the later processes
recover the earlier context from the Room, and no human copies event content
between them. E3 remains planned.

### S7 — human intervention

The human is an admitted Actor and uses the same append surface:

```bash
tearoom room join owner
tearoom send --as owner --json '{"correction":"use the release branch"}'
tearoom read --as implementer --after e3
```

The next Agent must observe the correction in sequence before continuing. The
intervention fails the story if it exists only in a private prompt or vendor
transcript. E4 remains planned.

### S5 — remote Room ownership and Agent revocation

This journey uses the
[target remote commands](reference/cli.md#target-command-contract--not-shipped):

```bash
tearoom auth login --server https://tearoom.example
tearoom room init --server https://tearoom.example --as owner
export TEAROOM_SERVER=https://tearoom.example/r/room-id
tearoom room join bob
tearoom room credential issue --actor bob --scope read,send --expires 30d

TEAROOM_ACTOR=bob TEAROOM_TOKEN=tr_example tearoom send "remote handoff"
tearoom room credential revoke credential-id
TEAROOM_ACTOR=bob TEAROOM_TOKEN=tr_example tearoom read
```

Login and initialization return the human account and Room; credential issue
returns the token once. Bob can read and send before revocation. The final read
must fail as an authorization error, while the owner, other credentials, Room,
Actors, and prior Events remain unchanged. E7 must run this same journey on
local SQLite and remote PostgreSQL after profile bootstrap, without changing
the Room, membership, send, read, credential, or failure behavior.

### S6 — public second-domain stress

The referee admits players and a spectator; all phases and public actions use
ordinary `send --json`, and the spectator uses ordinary `read`. The story
passes only when the complete public round replays in order without a game
command or game entity. Secret roles are deliberately absent; needing them
requires another Room or an external authority. E6 remains planned.

### S9 — snapshot sharing and continuation

This journey uses the
[target context-link commands](reference/cli.md#context-links-and-continuation):

```bash
tearoom link create --after e42 --limit 20
curl -fsSL -H 'Accept: text/plain' https://tearoom.example/l/link-id
tearoom auth login --server https://tearoom.example
tearoom room init --from https://tearoom.example/l/link-id --as receiver
```

The first command returns the immutable resolved range. The same URL renders a
human replay in a browser and readable text to the plain HTTP request. The last
command creates a new Room with the snapshot as its starting history; ordinary
`send` and `read` then continue there. Chat only carries the URL. E8 must
test the read-only snapshot before it tests continuation.

## Story viability

The product thesis is tested by S2 and S7 first, with S1 and S3 as necessary
primitives. Delivery is a paste or a context link, and S9 carries the
differentiation hypothesis as the distribution primitive. S5 proves that remote
deployment remains the same product. S6 tests generality without making games
the market line. S8 remains deferred until organization ownership is real.

| Story | Multi-turn journey | Falsifier |
|---|---|---|
| S1 | Alice appends; Bob joins later; Carol reads the same order; Bob contributes | Ordinary IM or a shared file gives equal replay and integration cost — **executed 2026-09-02; the shared-file rival matched. See [lessons/rival-falsification.md](lessons/rival-falsification.md)** |
| S2 | Planner proposes; another product acts; verifier rejects; the loop revises | One orchestrator, PR thread, or Agent workspace preserves context more simply |
| S3 | One process reads from an explicit boundary; a later process replays from that boundary | Repeated reads differ or silently change another caller's result |
| S5 | An owner creates a remote Room, grants one stable Agent limited access, then revokes it | The remote profile changes Room/Event behavior, runtime/session changes identity, or revoked credentials retain access |
| S6 | Referee publishes phases; players append public actions; spectator replays | Secret roles or authoritative settlement must be hidden in the single Room |
| S7 | Human correction is appended; a later Agent reads it before continuing | Correction exists only in a prompt, vendor transcript, or private control channel |
| S9 | A link pasted into a chat resolves for the receiving human and their agent; the agent's result returns as Events and a new link | Handoffs stay inside one agent product, so vendor share links suffice; or receivers only view and never continue, so the link degenerates into a screenshot |

## Evaluations

| ID | Evaluation | Target | Script | Status |
|---|---|---|---|---|
| E1 | P0 Room contract: dense append order, complete member history, and pure reads from explicit boundaries | `make target-room` | [target-room.sh](../scripts/target-room.sh) | shipped |
| E2 | P0 process/session replay without server-owned read progress | `make target-replay` | [target-replay.sh](../scripts/target-replay.sh) | shipped |
| E3 | P1 three-round cross-product handoff without kernel event types or implicit read state | `make target-handoff` | — | planned |
| E4 | P1 human intervention followed by ordered replay and continuation | `make target-intervention` | — | planned |
| E6 | P2 public-only Werewolf round with referee, players, and spectator replay | `make target-werewolf` | — | planned |
| E7 | P2 deployment parity across SQLite/PostgreSQL, shared authorization, stable Actors, and per-credential revocation | `make target-remote` | — | planned |
| E8 | P2 context-link journey: snapshot resolves on both faces, continuation append preserves the contract, chat stays transport | `make target-link` | — | planned |

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

## Hand-run evidence

Evidence that cannot run in `make check` — hand-run journeys, market
comparisons, counterfactuals against other products — is recorded here or it
never ran. One row per run: date, what ran, outcome, and where the detail
lives. Pending-evidence items above land in this table when they are executed.

| Date | Evaluation | Cases | Outcome | Record |
|---|---|---|---|---|
| 2026-09-02 | S1 shared-file rival and the offline-activation rival | Four claimed advantages, each tested against the cheapest rival, measured on both sides | S1 falsifier executed: the rival matched replay and integration cost; incremental reads, per-participant boundaries, concurrent-append safety, and in-product activation were each matched or beaten | [lessons/rival-falsification.md](lessons/rival-falsification.md) |

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
| `make hosted-demo` / `hosted-demo.sh` | Deleted with the separate hosted implementation; E7 now requires one shared service across deployment profiles |
| `make pg-smoke` / `pg-smoke.sh` | Deleted; PostgreSQL validation is the gated `TEAROOM_TEST_PG` store battery |

A green `make check` therefore proves the target P0 contract, not the old
implementation.

## Pending evidence, not stories

| Item | Evidence needed |
|---|---|
| PostgreSQL validation | Run the target Store battery when a database is available; backend parity does not prove product differentiation |
| Ordinary IM counterfactual | Compare the same P1 journey with a normal chat channel, shared workspace, and queue |
| Context-link distribution | Run the S9 journey end to end and count handoffs that stay inside one product versus cross-product, and receivers who continue versus only view |
