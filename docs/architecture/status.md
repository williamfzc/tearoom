---
type: architecture
title: Status and clean-break migration
description: The current implementation, the adopted Room/Event target, and the staged cutover with no product compatibility layer.
tags: [architecture, status, migration, clean-break]
timestamp: 2026-08-30
---

# Status and clean-break migration

The repository currently implements the pre-convergence model. The adopted
target is the minimum Room/Event model in
[model-discussion.md](model-discussion.md): membership determines visibility;
`to` is public addressing; reads have explicit caller boundaries; ordinary
append does not depend on a saved cursor; and offline Agent activation is an
optional application capability.

This is a clean break. The new release will not read the old Event schema,
accept old CLI spellings, accept the old HTTP request/response vocabulary, or
maintain a compatibility projection. Rollback is an operator action on the Git revision/deployment and,
where storage has already been cut over, its pre-cutover data snapshot. It is
never an in-product compatibility mode.

## Current implementation boundary

The code presently proves the old contract:

- `internal/core/core.go` defines `Event.ID`, `Room`, `Type`, `Body`, `Data`,
  `To`, `CausedBy`, and `Key`; `Event.VisibleTo` filters directed Events;
  `AppendInput.Expected` and `StaleWriteError` enforce room-head freshness.
- `internal/store/file.go` and `internal/relational/store.go` persist the old
  Event fields, auto-admitted Actors, directed visibility, idempotency keys,
  and expected-cursor append.
- `internal/cli/cli.go` owns `session`, `TEAROOM_HOME` state, automatic Actor
  registration, `event send|list|watch`, and `--cursor`; its read and write
  paths advance or consume the saved cursor.
- `internal/transport/client.go` and `server.go` implement the old
  `/api/room`, `/api/actors`, and `/api/events?actor=&after=&limit=&wait=`
  contract, including stale-write wire errors and long-poll support.
- `internal/control` and `internal/hosted` gate the current Store contract
  with bound credentials; they do not provide offline Agent activation.

The current tests and demos are evidence for this implementation only. They
must be replaced or rewritten as each clean-break stage lands; keeping them
green by preserving old semantics is not a migration criterion.

## Clean-break stages

Stages 1–4 are work checkpoints on one cutover branch, not separately supported
releases. They may temporarily break downstream compilation while dependency
owners are replaced in order. Nothing merges to or deploys from the release
branch until Stage 4 and the P0 evaluations pass as one coherent system. Stage
5 is a later optional product increment after that atomic cutover.

### Stage 0 — cutover contract and release boundary

Freeze the adopted target in `model-discussion.md`, this status, and the
reference docs. Record the last old revision and take an operator-owned backup
of any data that must remain available to the old release. Create a cutover
branch and deployment artifact.

**Done when:** the target Event shape and read/append semantics are explicit;
the old revision is named; the cutover branch exists; no compatibility
requirement is accepted.

**Rollback:** restore the old Git revision/deployment and its pre-cutover data.
No code change occurs in this stage.

### Stage 1 — replace the foundation contract

Change `internal/core` first. Define the minimum Event as `seq`, `actor`, `at`,
optional `to`, and `content`; derive presentation address `e<seq>` rather than
storing the old Event ID. Replace `Feed` with a repeatable read result that has
no implicit cursor. Make membership/admission explicit and make ordinary
append independent of read progress. Retain an explicit idempotency request
identity only if the new Store contract adopts it; it is not an Event field.

Delete `StaleWriteError`, `ErrStaleWrite`, `AppendInput.Expected`,
`Event.VisibleTo`, and the old Event fields and helpers. Do not add a second
schema or dual decoder.

**Done when:** `core` compiles only against the new contract and its unit tests
prove Room-wide member visibility, public `to`, ordered append, repeatable
explicit-boundary reads, and any explicitly retained idempotency behavior.

**Rollback:** revert the branch/deployment before publishing a new data format.

### Stage 2 — cut over persistence

Update `internal/store` and `internal/relational` to implement the new
`core.Store`. Store only the new Event representation; remove directed read
filtering, auto-registration, saved-cursor freshness checks, and old schema
fields. Make membership admission an explicit operation. Version the new file
layout and relational schema as a new cutover, not as a reader that understands
both formats. There is no old/new double write and no in-product importer.

**Done when:** file and relational backends pass the same new Store contract,
the old records are not accepted by the new release, and SQLite/PostgreSQL
validation covers ordering, membership visibility, restart, and explicit read
boundaries.

**Rollback:** deploy the prior revision against the pre-cutover snapshot. A
new-format dataset is not made readable by the old binary through a shim.

### Stage 3 — replace transport and hosted admission

Rewrite `internal/transport/client.go` and `server.go` around the new Store
contract. Replace the old query/body/response vocabulary, stale-write wire
error, actor cursor parameters, and server-held wait protocol. Resource paths
may keep their target names from [http-api.md](../reference/http-api.md), but no
handler accepts the old shape. A wait, if retained, is invocation-scoped and
never creates a durable cursor or subscription.

Update `internal/control` and `internal/hosted` so credentials authorize an
already admitted `(room, actor)` without silently creating membership. Hosted
visibility follows membership; `to` never becomes a secrecy or process-start
mechanism. No old HTTP route or response is served after this stage.

**Done when:** local and hosted clients use only the new request contract, negative tests
prove unjoined access is rejected, all admitted members see the full history,
and a raw old request fails normal target validation.

**Rollback:** redeploy the old server and client together. Do not route old and
new HTTP shapes to different Store readers in one process.

### Stage 4 — replace the CLI and client state

Change `internal/cli` and `cmd/tearoom` to expose the target common path:
`tearoom send` and `tearoom read`, with `--after` and optional invocation
`--wait`. Keep `room`, `server`, and hosted lifecycle commands only where they
remain part of the target product. Remove the `event` command group rather
than retaining aliases.

Delete `session`, `sessionPath`, `readSession`, `writeSession`, `TEAROOM_HOME`
cursor files, automatic `prepare` registration, `event list`, `event watch`,
`--cursor`, `--caused-by`, and old typed/body/data Event flags. Do not migrate
old session files or silently reinterpret them. Update the skill, README,
reference CLI, demos, and evaluations to the new command names in the same
cutover.

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

The clean break deletes, rather than aliases, these old surfaces:

| Boundary | Delete |
|---|---|
| Core | `StaleWriteError`, `ErrStaleWrite`, `Event.VisibleTo`, `Event.ID`/`Room`/`Type`/`Body`/`Data`/`CausedBy`/`Key`, `AppendInput.Expected`, cursor-bearing `Feed` |
| File/relational Store | old JSONL/relational Event shape, auto-registration, directed filtering, expected-cursor append path |
| CLI | `event` group, `list`, `watch`, all legacy aliases, session cursor files, `TEAROOM_HOME` cursor state, `--cursor`, `--caused-by`, and old Event-shape flags |
| HTTP | old actor/cursor query parameters, old append body and response envelope, `ReadWait`, stale-write wire errors, and old long-poll semantics |
| Hosted/application | silent membership creation and any activation behavior in the coordination Store |
| Verification/docs | old demos, fixtures, schemas, examples, skill instructions, and references that assert cursor or directed visibility |

No item in this table is kept as a deprecated alias, dual decoder, dual writer,
shadow projection, or compatibility route.

## Dependency direction after cutover

```text
foundation: core (Room, Actor, Event, Store)
       ^
persistence: file and relational Stores
       ^
application: hosted admission and optional activation ledger/launcher
       ^
outside: transport, CLI, process entry, operator integrations
```

The arrows mean “depends on.” `core` imports no project package. Persistence
implements `core.Store`; transport adapts it; hosted control gates admission;
the CLI translates commands. Activation may consume and append through the
Store, but the kernel never imports a launcher, scheduler, Agent runtime, or
hosted control plane. A rollback must preserve this direction rather than
reintroduce a deprecated layer.

## Overall completion boundary

The migration is complete only when the new core, both Stores, transport,
hosted admission, CLI, skill, tests, demos, and OKF docs agree; `make check`
and `make docs` pass; and repository searches find none of the deletion-manifest
surfaces. A green test run against old semantics is not completion.
