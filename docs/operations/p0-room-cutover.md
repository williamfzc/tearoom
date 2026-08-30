---
type: operations
title: P0 Room clean-break — executable cutover plan
description: The single execution contract for the P0 clean break: locked contract, implementation waves, test loop, deletion manifest, and final acceptance.
tags: [operations, migration, clean-break, cutover, plan]
timestamp: 2026-08-30
---

# P0 Room clean-break — executable cutover plan

This document is the execution contract for the P0 clean break defined in
[architecture/status.md](../architecture/status.md). It is the single source of
truth for the cutover task: a goal-driven agent may execute this document end to
end. It refines, and at cutover supersedes, the stage texts in status.md; the
design rationale remains in
[architecture/model-discussion.md](../architecture/model-discussion.md).

## Goal

Migrate the repository in one clean break from the pre-convergence model to a
usable minimum Room base, with no compatibility layer:

```text
Agent creates Room
→ creator auto-joins
→ other local Agents join explicitly
→ all members read and write one complete history
→ callers own their read boundaries
```

P0 delivers local file, SQLite, loopback HTTP, and PostgreSQL Store validation
only. Activation, hosted operation, cross-user invitations, account login, and
game protocols are out of scope. All execution, testing, and repair is done by
the Luna A/B/C subagents, which run on a mid/low-tier model; the main Agent
only schedules, reviews, and performs final acceptance.

## Execution model

- Every agent works in an isolated worktree. The integration branch is
  `codex/p0-room-cutover`; nobody edits `main` directly.
- The Luna subagents are mid/low-tier models; every step in this contract must
  be executable literally from the text above, without relying on high-end
  reasoning.
- Luna C integrates: it cherry-picks in the dependency order below, runs the
  integration loop, and commits Conventional Commits.
- If cherry-pick integration produces repeated conflicts, collapse Luna A and
  Luna B into one sequential agent before slowing the cutover down.

Integration order (fixed):

```text
core
→ file + relational
→ transport
→ CLI
→ legacy deletion
→ evaluations + docs
```

## Locked public contract

### Domain and Store

```go
type Actor struct {
    ID     string
    Joined time.Time
}

type Event struct {
    Seq     uint64
    Actor   string
    At      time.Time
    To      []string
    Content json.RawMessage
}

type AppendRequest struct {
    To             []string
    Content        json.RawMessage
    IdempotencyKey string
}

type Store interface {
    Room(ctx) (*Room, error)
    AdmitActor(ctx, actorID) (*Actor, error)
    Actors(ctx) ([]Actor, error)
    Read(ctx, actorID, after, limit) ([]Event, error)
    Append(ctx, actorID, request) (*Event, error)
}
```

Semantics:

- `content` accepts arbitrary JSON; plain text is encoded by the CLI as a JSON
  string.
- Content is compacted for storage and for idempotency comparison; the
  fingerprint compares compacted bytes, Actor, and normalized `to`.
- The idempotency key is persisted per `(room, actor, key)` but never becomes
  an Event field.
- No key: repeated appends produce distinct Events. Same key + same fingerprint:
  return the original Event. Same key + different fingerprint: conflict (409).
- Every member reads the same complete history; `to` is public addressing only.
- `after` is an exclusive caller-owned sequence; the Store never persists a
  cursor.
- Concurrent ordinary appends all succeed and receive a dense sequence.
- Delete `StaleWriteError`, `ErrStaleWrite`, `AppendInput.Expected`,
  `Event.VisibleTo`, `core.Probe`, the transport `Broadcaster`, and the old
  Event fields (`ID`, `Room`, `Type`, `Body`, `Data`, `CausedBy`, `Key`) and
  Actor fields (`Role`, `LastSeen`).
- The Room surface (which of `ID`/`Name`/`Created` survives; the CLI keeps no
  `--name` flag) is fixed by Luna A in the core contract before Luna B starts,
  per the adopted model in model-discussion.md.
- Wait lives in the transport layer, invocation-scoped, with its own change
  signal (polling is fine); it never enters `core.Store` and never creates a
  cursor or subscription.

### CLI

```text
tearoom room init --as planner [--store file|sqlite]
tearoom room join --as developer
tearoom room members

tearoom send "review this" --as planner [--to developer]...
tearoom send --json '{"plan":"..."}' --as planner
tearoom read --as developer [--after e42] [--limit N] [--wait 5m]
```

- `room init` atomically creates the Room and joins the creator.
- `room join` is explicit admission within the trust domain of one OS user.
- Actor comes from `--as` or `TEAROOM_ACTOR`.
- JSON output by default: `send` prints one Event; `read` prints JSONL, one
  Event per line, no envelope and no cursor.
- An empty `read` prints nothing and exits 0; `read --wait` timeout exits 3.
- `read` with both a boundary/limit and `--wait` returns as soon as one Event
  past `after` exists (or `--limit` is reached), otherwise waits until the
  bound elapses.
- Keep optional `--idempotency-key`; delete the `event` group, `list`, `watch`,
  session files, `TEAROOM_HOME`, and `--cursor`, `--type`, `--body`, `--data`,
  `--caused-by`, `--id`.
- `server start` listens only on `127.0.0.1:8787`; P0 rejects non-loopback
  addresses.

### HTTP

```text
GET  /api/room
GET  /api/actors
POST /api/actors
GET  /api/events?after=42&limit=100&wait=30
POST /api/events
```

- `X-Tearoom-Actor` declares the read/send Actor.
- `Idempotency-Key` is an optional append header.
- The append body carries only `to` and `content`; old fields return 400.
- `read` returns an Event array; there is no Feed and no cursor.
- POST actors is explicit join under loopback/OS trust.
- Unified error body: `{"error":{"code":"...","message":"..."}}`.
- 409 means idempotency-key conflict only; the CLI maps it to exit code 1.

## Implementation waves

### Luna A — Foundation and file Store

- Rewrite the `internal/core` contract and its contract tests.
- Rewrite the file Store: explicit admission, dense append, pure reads,
  idempotency records.
- `.tearoom/store.json` uses `format: 2`; old formats report incompatible —
  no import, no automatic upgrade.
- `events.jsonl` keeps an internal record envelope (Event plus request
  idempotency metadata); externally only Events are exposed.

### Luna B — Relational and transport

- After the core commit: rewrite the relational schema, SQLite/PostgreSQL
  transactions, and the format gate. Use a separate `append_idempotency`
  table; refuse to open old databases lacking the format-2 marker.
- Rewrite the HTTP client/server: strict JSON, Actor header, Event arrays,
  invocation-scoped wait.
- file, SQLite, and PostgreSQL share one Store conformance battery.

### Luna C — CLI, integration, evaluations, docs

- Implement the new CLI; delete session/cursor/old commands.
- Create the executable `target-room` and `target-replay` evaluations.
- Delete the old smoke, session, harness, game, worktree, and hosted demos,
  including `scripts/pg-smoke.sh` and `scripts/smoke-sqlite.sh` — PostgreSQL
  validation lives in the gated Go battery, and SQLite is covered by
  `target-room`. Remove the matching Makefile targets; the final `make check`
  chain is `fmt vet test test-race target-room target-replay check-evals docs`.
- Delete the hosted implementation and its assets: `internal/control`,
  `internal/hosted`, the hosted CLI, the `web/` SPA, Render/Docker deployment
  configs, and the old deploy runbook.
- Rewrite `docs/architecture/status.md` itself: Stage 3's hosted text becomes
  "delete the hosted implementation, keep the product design as deferred";
  the dependency diagram drops the hosted application layer; the deletion
  manifest's Hosted row becomes the full deletion list; the completion
  boundary wording is updated.
- Rewrite the reference docs: `docs/reference/cli.md`, `http-api.md`,
  `storage-layout.md`, `exit-codes.md` (exit 3 stays documented),
  `docs/operations/serving.md`, and `README.md`; update `docs/product.md`.
  Keep `docs/lessons/` and the architecture rationale docs.
- Update the Hosted product design (hosted-plan.md, hosted-identity.md,
  security.md) to state that a future hosted phase is secured by a user login
  identity, not by Actor-as-secret. Update activation.md only in its
  dependency and identity statements — no implementation.
- Update evaluations.md: E1/E2 become `shipped` with `make target-room` /
  `make target-replay`, S1/S3 become `proven`, targets are wired into the
  `make check` chain, and each evaluation script carries its
  `# Evaluation: E<id>` marker.
- Cherry-pick in the integration order, run the integration loop, and commit
  Conventional Commits.

## Test and repair loop

Luna C runs the complete loop continuously; any failure is returned to the
owning wave and fixed until the whole set passes consecutively:

1. `go test ./...`
2. `go test -race ./...`
3. `make target-room`
4. `make target-replay`
5. PostgreSQL Store battery — run when `TEAROOM_TEST_PG` is set; record the
   skip explicitly otherwise
6. `make check-evals`
7. `make docs`
8. `git diff --check`
9. Deletion-manifest `rg` checks
10. A final whole `make check` (the post-deletion chain above)

`target-room` must prove: explicit join over file, SQLite, and HTTP; read/send
rejected for unjoined Actors; multi-Actor concurrent append yields a dense
sequence; all members read the same history; `to=bob` remains visible to Carol;
repeated reads have no side effects; ordinary append does not depend on a prior
read; idempotent retry and conflict semantics are consistent.

`target-replay` must prove: different real processes use the same Actor
identity; a process can replay from an explicit `eN` boundary after exit; no
session/cursor files exist; `read --wait` affects only the current invocation;
Room history is consistent across restarts.

When the loop passes, evaluations.md records E1/E2 as shipped and S1/S3 as
proven; P1, Activation, and Hosted stay open/deferred.

## Deletion manifest

Delete, rather than alias, every surface below. After cutover, scoped `rg`
searches over shipped runtime code and normative CLI, product, and reference
entry points must find none of them. Migration manifests (including this
document), historical rationale, evaluation legacy ledgers, and negative
rejection tests are evidence and are excluded from those searches; they may
retain removed names to explain the cutover:

| Area | Delete |
|---|---|
| Core | `StaleWriteError`, `ErrStaleWrite`, `Event.VisibleTo`, old Event fields, `AppendInput.Expected`, cursor-bearing `Feed`, `core.Probe` |
| Stores | old JSONL/relational Event shape, auto-registration, directed filtering, expected-cursor append path |
| Transport | old actor/cursor query parameters, old append body and response envelope, stale-write wire errors, old long-poll semantics, `Broadcaster` |
| CLI | `event` group, `list`, `watch`, session cursor files, `TEAROOM_HOME`, `--cursor`, `--type`, `--body`, `--data`, `--caused-by`, `--id` |
| Scripts and Makefile | `smoke.sh`, `smoke-sqlite.sh`, `session-smoke.sh`, `harness-demo.sh`, `game-demo.sh`, `worktree-demo.sh`, `hosted-demo.sh`, `pg-smoke.sh`, and their Makefile targets |
| Hosted | `internal/control`, `internal/hosted`, hosted CLI, `web/` SPA, `render.yaml`, `Dockerfile`, `.dockerignore`, `docs/operations/deploy.md` |
| Docs | stale normative reference pages listed above; the current completion boundary and search exclusions are canonical in [status.md](../architecture/status.md) |

No item is kept as a deprecated alias, dual decoder, dual writer, shadow
projection, or compatibility route. `internal/store` lock infrastructure and
the `TEAROOM_TEST_PG` Go battery gate are kept.

## Final acceptance and cutover

The main Agent performs independent acceptance:

- Review the integration-branch commits, dependency direction, deletion
  manifest, and docs as the source of truth.
- Manually, in a temporary directory: planner creates, developer joins,
  concurrent sends, repeated reads, cross-process replay.
- Independently rerun the full test matrix.
- Confirm no old commands, old schema, cursor, directed visibility, hosted
  code, or untracked files remain.
- On acceptance, instruct Luna C to fast-forward `main`; no push, no deploy,
  no changes to the live Render/Pages resources.
- Confirm the `main` working tree is clean and report commits, test results,
  and whether PostgreSQL was actually validated.

## Default assumptions

- The current commit is the rollback point for the old format; old data is
  restored by the old binary plus its snapshot.
- The new binary explicitly rejects old `.tearoom` state and old databases;
  there is no importer.
- Local OS file permissions are the P0 user login; cross-machine accounts,
  invitations, and revocation belong to the future hosted phase.
