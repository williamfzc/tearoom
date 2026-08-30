---
type: architecture
title: Persistence direction
description: How persistence stores the durable Room and Event history without leaking database or activation concerns into the kernel.
tags: [architecture, persistence, sqlite, postgresql, orm]
timestamp: 2026-08-30
---

# Persistence direction

The target contract remains `core.Store`: a durable Room with membership and
one ordered immutable Event history. Database records and ORM APIs stay behind
that boundary. SQLite is shipped; PostgreSQL validation is gated. The current
schema and transaction still implement the older cursor and directed-visibility
model described below as migration debt.

## Deployment matrix

| Product form | Storage choice | Purpose |
|---|---|---|
| Local single-user | File Store by default; SQLite optional | One-directory setup and durable local use |
| Self-hosted service | SQLite or PostgreSQL | Small deployments can remain simple; larger ones can share a database |
| Hosted service | PostgreSQL | Concurrent instances, backups, operations, and tenant growth |

Use one ORM-supported schema where SQLite and PostgreSQL semantics genuinely
agree. Backend-specific behavior must not leak into Room, Actor, Event, the
CLI, or the HTTP contract.

## Ownership

- `internal/core` owns Room, Actor, Event, Store, and invariant language.
- A relational persistence package owns ORM models, migrations, queries,
  transactions, and database-specific adaptation.
- The file Store remains an independent implementation until a separate
  migration decision removes it.
- CLI and HTTP depend on `core.Store`, never on ORM or concrete database types.
- A future activation service owns its delivery ledger; activation state is not
  part of the Room/Event kernel tables.

The ORM is replaceable infrastructure. Its model types must not be exported as
product entities. The shipped SQLite backend uses GORM with the pure-Go
`glebarez/sqlite` dialector, keeping release binaries static (`CGO_ENABLED=0`).

## Target relational shape

The minimum durable coordination data is:

- Rooms with identity, lifecycle metadata, membership, and an ordered head;
- Actors admitted to a Room with stable identity;
- Events with sequence, author, acceptance time, optional `to`, and content.

The exact content representation remains open. `type`, `caused_by`, or other
envelope fields may be protocol conventions later, but they are not required by
the minimum model. Hosted control-plane tables such as accounts, memberships,
credentials, and billing records remain outside coordination data; they
authorize access but do not change Event semantics.

Room membership is the visibility boundary. Every admitted member may read the
complete Room history. `to` is public addressing metadata, not an Event-level
secrecy filter. A private collaboration requires a separate Room and its own
membership.

## Target append and read contract

An ordinary append is serialized per Room and assigns the next dense sequence.
It validates the author and any addressed members, inserts the immutable Event,
and commits the Room head with the Event. It does not derive a mandatory write
precondition from a hidden or saved read cursor.

Reading is a repeatable query with a caller-supplied boundary such as `after
seq`; it does not silently advance Actor state. If reliable delivery progress
becomes necessary, an explicitly named consumer or activation identity owns
that state outside the Actor's identity.

Idempotent append may remain an explicit retry contract. If adopted, an
identical retry returns the original Event and reusing a key for different
content is a conflict. This is independent of read progress.

## Current transaction contract

The shipped file and relational Stores still perform the older transaction:

1. lock the Room head;
2. validate the Actor and recipients;
3. resolve an existing idempotency key;
4. compare the expected saved cursor with the current head;
5. assign the next sequence;
6. insert the immutable Event and advance the head;
7. commit as one unit.

The same implementations still filter directed Events and the CLI still
persists per-Actor cursors. These guarantees are tested legacy behavior, not
evidence that the target minimum model has shipped. A migration must change
both Store implementations and their callers together.

## Activation persistence

When optional activation is implemented, its ledger must be an application
boundary keyed by an Event reference and target Actor, for example:

```text
(room, event sequence, target actor, activation id)
```

It may record pending, claimed, leased, delivered, failed, and retry state. It
must not be confused with Room history, Actor membership, read progress, or a
claim that the Agent completed the collaboration. The Room/Event append path
remains usable with no activation tables or runtime bindings.

## Migration discipline

Schema changes are versioned migrations committed with the code that needs
them. Migrations run before serving traffic and are tested against SQLite and
PostgreSQL before a hosted release.

A future importer must preserve Room identity, Actor identity, Event ids and
sequence numbers, timestamps, addressing, and any adopted idempotency keys.
Until that importer exists, file and relational Stores are separate creation
choices rather than silently interchangeable formats.

## Related

- [model-discussion.md](model-discussion.md)
- [layers.md](layers.md)
- [hosted-plan.md](hosted-plan.md)
- [../domain/hosted-identity.md](../domain/hosted-identity.md)
- [../reference/storage-layout.md](../reference/storage-layout.md)
