---
type: architecture
title: Persistence direction
description: How one service stores the durable Room and Event history across local and remote deployment profiles.
tags: [architecture, persistence, sqlite, postgresql, orm]
timestamp: 2026-09-10
---

# Persistence direction

The target contract remains `core.Store`: a durable Room with membership and
one ordered immutable Event history. Database records and ORM APIs stay behind
that boundary. SQLite is shipped; PostgreSQL validation is gated. They are
database adapters for one service, not separate product implementations.
Implementation status belongs in [status.md](status.md); this document defines
only the persistence target and clean-break boundary.

## Deployment matrix

| Deployment profile | Storage choice | Purpose |
|---|---|---|
| Local | SQLite (the only local backend) | One-machine setup and durable local use |
| Remote | PostgreSQL | Concurrent instances, backups, operations, and tenant growth |

Use one relational schema owner and migration sequence where SQLite and
PostgreSQL semantics genuinely agree. The same service and acceptance journeys
must run against both. Backend-specific behavior must not leak into Room,
Actor, Event, identity decisions, the CLI, or the HTTP contract.

## Ownership

- `internal/core` owns Room, Actor, Event, Store, and invariant language.
- `internal/relational` owns ORM models, migrations, queries, transactions,
  and database-specific adaptation.
- CLI and HTTP depend on `core.Store`, never on ORM or concrete database types.
- Delivery state, if an external operator needs it, is not part of the
  Room/Event kernel tables.

The ORM is replaceable infrastructure. Its model types must not be exported as
product entities. The shipped SQLite backend uses GORM with the pure-Go
`glebarez/sqlite` dialector, keeping release binaries static (`CGO_ENABLED=0`).

## Target relational shape

The minimum durable coordination data is:

- Rooms with identity, lifecycle metadata, membership, and an ordered head;
- Actors admitted to a Room with stable identity;
- Events with sequence, author, acceptance time, optional `to`, and content as
  arbitrary valid JSON.

Content is stored and transported as compact JSON; plain text is represented as
a JSON string. `type`, `caused_by`, or other envelope fields may be protocol
conventions later, but they are not required by the minimum model. Target
service records for owner identities and Agent credentials remain outside
coordination data; when implemented, they authorize access but do not change
Event semantics. Local bootstrap and remote login will feed the same
authorization path.

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
becomes necessary, an explicitly named consumer identity owns
that state outside the Actor's identity.

Idempotent append is an optional append capability, not part of the minimum
Event shape. An identical retry returns the original Event and reusing a key
for different content is a conflict. The key is request metadata, not an Event
field, and is independent of read progress.

## Clean-break storage cutover

The file and relational Stores change together after the new `core.Store`
contract exists. The new release stores only the minimum Event shape and does
not read or write the pre-adoption representation. There is no importer, dual
decoder, shadow table, or old/new double write. Data worth retaining is exported
before cutover; rollback restores the previous binary with its matching data
snapshot.

## Delivery persistence

No activation ledger will be implemented in this repository (adjudicated
2026-09-09); an external deliverer owns its own persistence. The boundary it
must respect stands: delivery state is never Room history, Actor membership,
read progress, or a claim that the Agent completed the collaboration. The
Room/Event append path remains usable with no activation tables or runtime
bindings; the refusal and its reopening condition are recorded in
[../product.md](../product.md).

## Migration discipline

Schema changes are versioned migrations committed with the code that needs
them. Migrations run before serving traffic and are tested against SQLite and
PostgreSQL before a remote-capable release. The pre-cut file layout is retired,
not imported: an old `.tearoom` directory is an incompatible store, and
rollback uses the previous release with its data snapshot.

## Related

- [layers.md](layers.md)
- [status.md](status.md)
- [../reference/storage-layout.md](../reference/storage-layout.md)
