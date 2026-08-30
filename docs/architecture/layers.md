---
type: architecture
title: Layers and dependency direction
description: The post-cutover package boundaries and the removal of legacy cursor, visibility, and activation leakage.
resource: internal/
tags: [architecture, boundaries, layering, migration, clean-break]
timestamp: 2026-08-30
---

# Layers and dependency direction

The clean-break target has one foundation contract and no compatibility layer:

| Layer | Package or owner | Owns after cutover |
|---|---|---|
| Foundation | `internal/core` | Room, Actor, minimum Event, Store, and typed domain errors |
| Persistence | `internal/store`, `internal/relational` | Durable membership and ordered immutable Event history |
| Application | future activation service (hosted control plane deferred) | Admission policy, credentials, activation ledger, leases, and runtime binding |
| Outside | `internal/transport` | The one current HTTP representation of the Store contract |
| Outside | `internal/cli`, `cmd/tearoom` | `room`, `server`, `send`, `read`, output, and process entry |

Dependencies point inward. `core` imports no project package. Persistence
implements `core.Store`; transport adapts it; the CLI translates commands. A
future hosted control plane would authorize admission outside the Store. Optional
activation may consume and append through `core.Store`, but the kernel never
imports a launcher, scheduler, Agent runtime, or hosted control plane.

## Target foundation boundary

`internal/core` must define only the shared collaboration truth:

- Room identity and lifecycle;
- stable Actor identity and explicit membership/admission;
- an Event containing `seq`, `actor`, `at`, optional `to`, and `content`;
- ordered append and explicit-boundary read operations;
- typed errors that describe the new contract.

Membership determines visibility. Every admitted member reads the complete Room
history. `to` is public addressing or attention metadata, not secrecy and not a
process-start request. Event address `e<seq>` is presentation derived from the
Room sequence; it is not a second Event identity.

The target foundation must not contain workflow, task, game, thread, lease,
subscription, unread, or runtime entities. Idempotency may remain an explicit
append request concern if the new Store contract adopts it; it must not become
another Event field.

## Persistence boundary

`internal/store` owns the file layout and cross-process locking. The relational
package owns ORM models, migrations, transactions, and database adaptation.
Both implement the same new `core.Store` contract and store only the new Event
shape. Neither package may:

- filter Events by `to`;
- create membership as a side effect of reading or sending;
- compare an append with a hidden Actor cursor;
- persist or interpret the old Event representation;
- host activation claims or process state.

The pre-cutover implementations violated these target rules: they contained
`Event.VisibleTo`, auto-registration, and expected-cursor append. Stages 1–2 of
the cutover in [status.md](status.md) removed those behaviors rather than
wrapping them.

## Application boundary

Hosted control is a deferred application boundary around the Store (the
implementation was deleted at the cutover; the design is recorded in
[hosted-plan.md](hosted-plan.md)). When it returns, it resolves credentials to
an already admitted Room/Actor and applies capabilities; it does not rewrite
Event semantics. Membership is admitted explicitly and is the visibility
boundary.

Offline activation is a separate optional application component:

```text
addressed Event -> policy -> durable activation claim/lease
                -> launcher -> ephemeral Agent Runtime
                -> Runtime reads Room and appends a contribution
```

Its ledger may contain attempt, lease, retry, budget, and failure state keyed by
an Event reference and target Actor. It must not be attached to Actor read
progress or treated as proof that the Event was understood or completed.
Activation is at-least-once; external effects need their own idempotency or
approval contract. Without this component, `send` ends at durable append.

## Outside boundaries

`internal/transport` exposes only the new HTTP contract. It may use the target
resource paths, but it must not preserve actor/cursor query semantics, the old
append or response shape, stale-write wire errors, or deprecated wait behavior.

`internal/cli` exposes only the target common commands:

```text
tearoom room init|join|members
tearoom server start
tearoom send <content> [--to <member>]...
tearoom read [--after e<seq>] [--limit <count>] [--wait <duration>]
```

It owns formatting and invocation-local options, not shared invariants. The
old `event` command group, session cursor files, `TEAROOM_HOME` cursor state,
automatic registration, and old flags are deleted. No alias or parser branch
may keep them alive.

## Clean-break dependency checks

After the cutover, the following must hold:

1. `core` has no import path to persistence, transport, CLI, control, or
   activation.
2. Both Stores satisfy one new interface and have no old/new reader or writer.
3. No Store operation silently admits an Actor: membership is explicit, and
   authorization (today absent on loopback, in the future hosted login
   identity) happens before Store access.
4. Transport has one route/body/error vocabulary.
5. CLI, skills, tests, demos, and docs use only the new command and Event
   vocabulary.
6. Activation is removable without changing Room visibility or append/read
   semantics.

The migration is complete only when `make check`, `make docs`, and repository
searches verify these boundaries. If a stage fails, rollback is a Git/deploy
rollback to a matched old revision and data snapshot; adding a deprecated
dependency or compatibility layer is not an allowed recovery.

## Related

- [model-discussion.md](model-discussion.md)
- [convergence.md](convergence.md)
- [persistence.md](persistence.md)
- [status.md](status.md)
- [../domain/concepts.md](../domain/concepts.md)
