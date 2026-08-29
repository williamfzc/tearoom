---
type: architecture
title: Persistence direction
description: How relational persistence supports local SQLite and hosted PostgreSQL without leaking ORM concerns into the kernel.
tags: [architecture, persistence, sqlite, postgresql, orm]
timestamp: 2026-08-28
---

# Persistence direction

Tearoom has a relational Store, and it must not become an ORM-shaped product. The canonical contract remains `core.Store`; database records and ORM APIs belong behind that boundary. The SQLite backend is shipped; PostgreSQL validation is pending.

## Target deployment matrix

| Product form | Default database | Purpose |
|---|---|---|
| Local single-user | SQLite | One-file setup, transactions, durable local use |
| Self-hosted service | SQLite or PostgreSQL | Small deployments may stay simple; larger ones may use a shared database |
| Hosted SaaS | PostgreSQL | Concurrent service instances, backups, operations, and tenant growth |

Use one ORM-supported schema where SQLite and PostgreSQL semantics genuinely agree. Backend-specific behaviour must not leak into Room, Actor, Event, the CLI, or the HTTP contract.

## Ownership

- `internal/core` owns Room, Actor, Event, Store, and invariant language.
- A relational persistence package owns ORM models, migrations, queries, transactions, and database-specific adaptation.
- The file Store remains an independent implementation until a separate migration decision removes it.
- CLI and HTTP depend on `core.Store`, never on the ORM or concrete database models.

The ORM is replaceable infrastructure. Its model types must not be exported as product entities. The shipped SQLite backend uses GORM with the pure-Go `glebarez/sqlite` dialector, keeping release binaries static (`CGO_ENABLED=0`).

## Relational shape

The coordination data needs only three durable tables:

- Rooms contain identity, name, creation metadata, and the current sequence head.
- Actors belong to a Room and carry stable identity plus optional role metadata.
- Events belong to a Room and carry sequence, author, type, body or JSON data, recipients, cause, idempotency key, and timestamp.

Hosted control-plane tables such as accounts, GitHub identities, organizations, memberships, credentials, and billing records remain outside this coordination schema. They authorize access to a Room; they do not change Event semantics.

## Transaction contract

One Event append transaction must:

1. identify and lock the Room head;
2. validate the Actor and all recipients;
3. resolve an existing idempotency key;
4. compare the expected cursor with the current head;
5. assign exactly the next sequence;
6. insert the immutable Event;
7. advance the Room head;
8. commit as one unit.

An identical retry returns the original Event. Reusing an idempotency key for different content is a conflict. A stale cursor is rejected before an Event is inserted.

These guarantees are acceptance criteria for every database backend. ORM convenience methods are not a substitute for an explicit transaction.

## Migration discipline

Schema changes are versioned migrations committed with the code that needs them. Migrations must run before serving traffic and must be tested against SQLite and PostgreSQL before a hosted release.

A future file-to-database importer must preserve Room identity, Actor identity, Event ids, sequence numbers, timestamps, recipients, causes, and idempotency keys. Until that importer exists, the file Store and relational Store are separate creation choices rather than silently interchangeable formats.

## Related

- [../product.md](../product.md)
- [layers.md](layers.md)
- [../domain/hosted-identity.md](../domain/hosted-identity.md)
- [../reference/storage-layout.md](../reference/storage-layout.md)
