---
type: architecture
title: Convergence
description: The clean-break command and protocol boundary for the minimum Room/Event model.
resource: internal/cli/cli.go
tags: [architecture, cli, convergence, migration, clean-break]
timestamp: 2026-08-30
---

# Convergence

[model-discussion.md](model-discussion.md) is the adopted design baseline. The
product is a durable shared Room with explicit membership, one ordered
immutable Event history, stable Actor identities, and optional Agent activation
above the kernel.

The cutover is intentionally clean. The new CLI will not retain aliases, the
new Store will not double-write or dual-read Event schemas, and the new HTTP
server will not accept the old request or response vocabulary. A prior release is recovered by
Git/deployment rollback, not by a compatibility layer.

## Shipped command surface

Low-frequency lifecycle commands remain separate from normal collaboration:

```text
tearoom room init [--as <actor>] [--store file|sqlite]
tearoom room join [--as <actor>]
tearoom room members
tearoom send <content> [--to <member>]... [--idempotency-key <key>]
tearoom read [--after e<seq>] [--limit N] [--wait <duration>]
tearoom server start [--address 127.0.0.1:8787]
```

`send` appends one contribution. `read` is a repeatable, side-effect-free
query. `--after` is supplied by the caller; no Actor session silently advances
or supplies a checkpoint. `--wait` lasts only for that invocation and does not
create a subscription. It is invocation-scoped transport behavior for the
current request, not the deleted durable Feed/ReadWait abstraction, and never
creates cursor or delivery state.

`to` is public addressing or attention metadata. All admitted Room members can
read the complete history; an addressed Event does not become private and does
not itself launch a process.

## Deleted boundary

The old surfaces in `internal/cli/cli.go` were removed at cutover, not
retained as aliases:

```text
tearoom event send|list|watch
```

The old `session` files under `TEAROOM_HOME`, automatic Actor registration,
`--cursor`, typed/body/data/causal Event flags, and freshness errors were
deleted along with the rest of the [deletion manifest](status.md). Examples,
skills, demos, and evaluations were rewritten in the same release.

## Target responsibilities

| Concern | Owner | Responsibility |
|---|---|---|
| Room lifecycle and membership | Room boundary; future hosted control plane | Admit and authorize Actors |
| collaboration history | `core.Store` implementations | Append and read ordered immutable Events |
| client interaction | CLI and transport | Translate `send`/`read`, never own shared invariants |
| offline activation | application service | Observe addressed Events, claim/lease, launch and retry |
| process execution | operator-owned launcher/runtime | Run an Actor and append its contribution |

Activation is optional. Without it, an addressed Event ends at durable append.
With it, the trigger references the Event; it does not copy context or mutate
Room visibility. Delivery state is explicit application state, never implicit
Actor read state or Event completion.

## Clean-break ordering

The layers must cut over in this order:

1. `internal/core` publishes the new Event, read, append, and membership
   contract.
2. `internal/store` and `internal/relational` implement it with a new storage
   shape; old records are not read and no old/new double write exists.
3. `internal/transport` publishes the new HTTP contract. A future hosted or
   control-plane implementation may add authenticated admission around the
   unchanged Store contract.
4. `internal/cli`, `cmd/tearoom`, skills, examples, tests, and evaluations
   publish only `send`/`read` and explicit boundaries.
5. An application-owned activation service is added only after the first four
   stages pass; it is not a kernel or transport prerequisite.

Each stage is incomplete until its callers and tests use the new contract. A
temporary compile adapter, deprecated alias, shadow schema, old route, or
dual-write path would make the stage internally dishonest and is prohibited.
Stages 1–4 run on one cutover branch and merge or deploy only as one coherent
P0 release; they are dependency-ordered work checkpoints, not public partial
versions.

## Completion and rollback

The migration is complete when scoped repository searches over shipped runtime
code and normative entry points find no old command, Event field, cursor
session, directed-visibility filter, stale-write route, or old HTTP shape; both
Stores and the local journey pass the new boundary tests; future hosted work
remains deferred; and `make check` plus `make docs` pass. Migration manifests,
historical rationale, evaluation legacy ledgers, and negative rejection tests
are evidence and are excluded from those searches.

Before the storage cutover, rollback means redeploying the previous Git
revision. After storage cutover, rollback means redeploying that revision with
the operator's preserved pre-cutover dataset. The new release never attempts to
interpret old session files or old Event rows, and the old release is never
asked to interpret the new format.
