---
type: architecture
title: Layers and dependency direction
description: The post-cutover package boundaries for one service across local and remote deployment profiles.
resource: internal/
tags: [architecture, boundaries, layering, migration, clean-break]
timestamp: 2026-09-09
---

# Layers and dependency direction

The clean-break target has one foundation contract and no compatibility layer:

| Layer | Package or owner | Owns after cutover |
|---|---|---|
| Foundation | `internal/core` | Room, Actor, minimum Event, Store, and typed domain errors |
| Persistence | `internal/store`, `internal/relational` | Durable membership and ordered immutable Event history |
| Application | shared service policy (not implemented) | Admission policy, identities, credentials, and management use cases |
| Outside | `internal/transport` | The one current HTTP representation of the Store contract |
| Outside | `internal/cli`, `cmd/tearoom` | `room`, `server`, `send`, `read`, output, and process entry |

Dependencies point inward. `core` imports no project package. Persistence
implements `core.Store`; transport adapts it; the target application layer will
apply the same admission and capability policy in every deployment; the CLI translates
commands. Local bootstrap and remote login are outside adapters into that one
policy. The kernel never imports a launcher, scheduler, Agent runtime, or
deployment-specific control plane.

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
- host delivery claims or process state.

The pre-cutover implementations violated these target rules: they contained
`Event.VisibleTo`, auto-registration, and expected-cursor append. Stages 1–2 of
the cutover in [status.md](status.md) removed those behaviors rather than
wrapping them.

## Target application boundary

The application layer will be shared by local and remote deployment. It resolves an
identity, applies admission and capabilities, and then calls the Store; it does
not rewrite Event semantics. Local setup may bootstrap an owner while remote
setup uses login and scoped credentials, but both enter the same policy path.
Membership remains explicit and is the visibility boundary. The current code
has only the loopback trust shortcut; the shared policy and remote adapters are
open work recorded in [status.md](status.md).

Delivery to an absent Agent is not an application component of this
repository (adjudicated 2026-09-09). An external operator or Agent product
starts and configures the receiving process with its Room connection. The
process then uses ordinary `read` and `send`. External process state must not
attach to Actor read progress or become proof that an Event was understood or
completed. Without a running receiver, `send` ends at durable append.

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

1. `core` has no import path to persistence, transport, CLI, service policy, or
   deployment adapters.
2. Both Stores satisfy one new interface and have no old/new reader or writer.
3. No Store operation silently admits an Actor: membership is explicit, and
   one authorization path handles both local bootstrap identity and remote
   credentials before Store access.
4. Transport has one route/body/error vocabulary.
5. CLI, skills, tests, demos, and docs use only the new command and Event
   vocabulary.
6. Switching SQLite for PostgreSQL, or local identity for remote login, does
   not change Room visibility or append/read semantics.

The migration is complete only when `make check`, `make docs`, and repository
searches verify these boundaries. If a stage fails, rollback is a Git/deploy
rollback to a matched old revision and data snapshot; adding a deprecated
dependency or compatibility layer is not an allowed recovery.

## Related

- [persistence.md](persistence.md)
- [status.md](status.md)
- [../domain/concepts.md](../domain/concepts.md)
