---
type: architecture
title: Status and clean-break migration
description: The current implementation, the adopted Room/Event target, and the completed staged cutover with no product compatibility layer.
tags: [architecture, status, migration, clean-break]
timestamp: 2026-08-30
---

# Status and clean-break migration

The repository now implements the minimum Room/Event model adopted in
[model-discussion.md](model-discussion.md): membership determines visibility;
`to` is public addressing; reads have explicit caller boundaries; ordinary
append does not depend on a saved cursor; and offline Agent activation is an
optional application capability.

This was a clean break. The new release does not read the old Event schema,
accept old CLI spellings, accept the old HTTP request/response vocabulary, or
maintain a compatibility projection. Rollback is an operator action on the Git
revision/deployment and, where storage has already been cut over, its
pre-cutover data snapshot. It is never an in-product compatibility mode.

## Current implementation boundary

The code proves the adopted contract:

- `internal/core/core.go` defines `Room`, `Actor`, `Event` (`seq`, `actor`,
  `at`, optional `to`, `content`), and the `Store` seam; the conformance
  battery in `contract.go` runs against every Store implementation.
- `internal/store/file.go` persists the new layout: explicit admission, dense
  append under flock, pure caller-owned reads, and per-request idempotency
  records.
- `internal/relational/store.go` implements the same contract on SQLite with a
  format-2 gate, and PostgreSQL remains a gated validation battery
  (`TEAROOM_TEST_PG`).
- `internal/transport` serves the new HTTP contract: Actor on
  `X-Tearoom-Actor`, Event arrays, unified error envelope, and
  invocation-scoped wait.
- `internal/cli` exposes `room init|join|members`, `send`, `read` (JSONL,
  `--after e<seq>`, `--wait`), and `server start` (loopback-only). It owns no
  session state.
- `scripts/target-room.sh` and `scripts/target-replay.sh` prove the P0 claims
  against the shipped binary and are wired into `make check`.

The hosted implementation (`internal/control`, `internal/hosted`, the hosted
CLI, the web SPA, and the deployment assets) was deleted at cutover; the
hosted product design remains as a deferred phase below.

## Clean-break stages

Stages 1–4 were work checkpoints on one cutover branch, not separately
supported releases. They landed as one coherent P0 release; nothing merges to
or deploys from the release branch until the P0 evaluations pass as one
system. Stage 5 is a later optional product increment after that atomic
cutover.

### Stage 0 — cutover contract and release boundary

Froze the adopted target in `model-discussion.md`, this status, and the
reference docs; recorded the last old revision; took an operator-owned backup
of any data that must remain available to the old release; created the cutover
branch and deployment artifact.

**Rollback:** restore the old Git revision/deployment and its pre-cutover data.
No code change occurs in this stage.

### Stage 1 — replace the foundation contract

Changed `internal/core` first: the minimum Event is `seq`, `actor`, `at`,
optional `to`, and `content`; the presentation address `e<seq>` is derived
rather than stored; `Feed` is replaced by a repeatable read result with no
implicit cursor; membership/admission is explicit; ordinary append is
independent of read progress; request-level idempotency is retained and never
becomes an Event field.

Deleted `StaleWriteError`, `ErrStaleWrite`, `AppendInput.Expected`,
`Event.VisibleTo`, and the old Event fields and helpers. No second schema or
dual decoder was added.

**Rollback:** revert the branch/deployment before publishing a new data format.

### Stage 2 — cut over persistence

Updated `internal/store` and `internal/relational` to implement the new
`core.Store`: only the new Event representation is stored; directed read
filtering, auto-registration, saved-cursor freshness checks, and old schema
fields are gone. Membership admission is an explicit operation. The file
layout and relational schema are versioned as a new cutover (`.tearoom`
`format: 2`), not as a reader that understands both formats. There is no
old/new double write and no in-product importer.

**Rollback:** deploy the prior revision against the pre-cutover snapshot. A
new-format dataset is not made readable by the old binary through a shim.

### Stage 3 — replace transport; delete the hosted implementation

Rewrote `internal/transport/client.go` and `server.go` around the new Store
contract: strict JSON, Actor header, Event arrays, the unified error envelope,
and invocation-scoped wait. No handler accepts the old shape; no old HTTP
route or response is served.

The hosted implementation was **deleted, not adapted**: `internal/control`,
`internal/hosted`, the hosted CLI group, the `web/` SPA, `render.yaml`, the
`Dockerfile`, `.dockerignore`, and the deploy runbook were removed from the
repository. The hosted product design stays in
[hosted-plan.md](hosted-plan.md) as a deferred, login-identity-based future
phase. A future hosted phase is secured by a user login identity, not by
Actor-as-secret.

**Rollback:** redeploy the old server and client together. Do not route old and
new HTTP shapes to different Store readers in one process.

### Stage 4 — replace the CLI and client state

Changed `internal/cli` and `cmd/tearoom` to expose the target common path:
`tearoom send`, `tearoom read` with `--after` and optional invocation
`--wait`, `room init|join|members`, and `server start`. The `event` command
group is removed rather than retained as aliases.

Deleted `session`, `sessionPath`, `readSession`, `writeSession`, `TEAROOM_HOME`
cursor files, automatic `prepare` registration, `event list`, `event watch`,
`--cursor`, `--caused-by`, and the old typed/body/data Event flags. Old
session files are never migrated or silently reinterpreted. The skill, README,
reference CLI, demos, and evaluations were updated in the same cutover.

**Done when:** `rg` finds no old command alias, cursor session path, or old
Event flag in shipped CLI/docs; multi-process and cross-worktree journeys use
explicit read boundaries and explicit membership.

**Rollback:** redeploy the old CLI and server as a matched release. The new CLI
never attempts to understand old session files.

### Stage 5 — optional activation proof

Only after Stages 1–4 are complete, add an application-owned activation
service. It may observe an addressed Event, claim `(room, seq, target actor)`,
lease and retry a wake attempt, and invoke an operator-owned runtime. The
ledger is not part of the Room/Event kernel, does not alter visibility, and
does not imply that the Agent read or completed the Event.

The canonical component, identity, state-machine, recovery, launch, and safety
contracts are in [activation.md](activation.md). This status records when that
design is implemented; it does not restate the design.

Prove one local offline-to-response journey, including daemon restart and
duplicate-safe at-least-once delivery. Do not add a scheduler, hosted wake
product, or replay workspace until this boundary is real.

**Done when:** a member can be offline, receive a durable Event reference after
restart, re-enter as its stable Actor, read the Room, and append a response;
the proof distinguishes runtime settlement from collaboration outcome.

**Rollback:** remove the activation deployment or revert its Git revision. The
Room kernel remains usable without activation state or a runtime binding.

## Deletion manifest

The clean break deleted, rather than aliased, these old surfaces:

| Boundary | Deleted |
|---|---|
| Core | `StaleWriteError`, `ErrStaleWrite`, `Event.VisibleTo`, `Event.ID`/`Room`/`Type`/`Body`/`Data`/`CausedBy`/`Key`, `AppendInput.Expected`, cursor-bearing `Feed`, `core.Probe` |
| File/relational Store | old JSONL/relational Event shape, auto-registration, directed filtering, expected-cursor append path |
| CLI | `event` group, `list`, `watch`, all legacy aliases, session cursor files, `TEAROOM_HOME` cursor state, `--cursor`, `--caused-by`, and old Event-shape flags |
| HTTP | old actor/cursor query parameters, old append body and response envelope, `ReadWait` server subscriptions, stale-write wire errors, old long-poll semantics, `Broadcaster` |
| Scripts and Makefile | `smoke.sh`, `smoke-sqlite.sh`, `session-smoke.sh`, `harness-demo.sh`, `game-demo.sh`, `worktree-demo.sh`, `hosted-demo.sh`, `pg-smoke.sh`, and their Makefile targets |
| Hosted | `internal/control`, `internal/hosted`, the hosted CLI, the `web/` SPA, `render.yaml`, `Dockerfile`, `.dockerignore`, `docs/operations/deploy.md` |
| Docs | normative product/reference pages contain no old contract; migration manifests, historical rationale, evaluation legacy ledgers, and negative rejection tests may name removed surfaces as evidence |

No item in this table is kept as a deprecated alias, dual decoder, dual writer,
shadow projection, or compatibility route.

## Dependency direction after cutover

```text
foundation: core (Room, Actor, Event, Store)
       ^
persistence: file and relational Stores
       ^
outside: transport, CLI, process entry, operator integrations
```

The arrows mean "depends on." `core` imports no project package. Persistence
implements `core.Store`; transport adapts it; the CLI translates commands.
The hosted application layer no longer exists in the repository. Activation
may consume and append through the Store, but the kernel never imports a
launcher, scheduler, Agent runtime, or hosted control plane. A rollback must
preserve this direction rather than reintroduce a deprecated layer.

## Overall completion boundary

The migration is complete only when the new core, both Stores, transport,
CLI, tests, evaluations, and OKF docs agree; `make check` and `make docs`
pass; and scoped searches over shipped runtime code and normative entry points
find none of the deletion-manifest surfaces. Those searches explicitly exclude
migration manifests, historical rationale, evaluation legacy ledgers, and
negative rejection tests, which preserve removed names as evidence. A green
test run against old semantics is not completion. The P0 evaluations
(`make target-room`, `make target-replay`) are the executable completion
proof; P1, Activation, and Hosted remain open or deferred in
[evaluations.md](../evaluations.md).
