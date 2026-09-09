---
type: product
title: Product definition
description: Authoritative definition of Tearoom's single user story, target model, deployment profiles, and completion standard.
tags: [product, scope, authority]
timestamp: 2026-09-10
---

# Product definition

This document is the only authority for product scope. The implementation gap
is in [status.md](architecture/status.md), executable proof in
[evaluations.md](evaluations.md), machine-facing behavior under
[reference/](reference/index.md), and dated decisions in [log.md](log.md).
Historical behavior does not redefine this document.

## One sentence

**Tearoom is a durable shared Room where one participant writes work, another
person or Agent product pulls the same ordered history through the CLI,
continues, and writes back for the next participant.**

## The one user story

When work moves between people, processes, or Agent products, the receiving
participant can recover the prior context and continue from the same record
without the sender manually rewriting or relaying it.

The complete journey has four steps:

1. Actor A appends work.
2. Actor B pulls the Room history through the CLI.
3. Actor B continues the work and appends the result.
4. Actor A or Actor C pulls the new result and can continue again.

A human correction is not a separate story. A human is an Actor and uses the
same read and append path. Process restart is not a separate story either; it
is a required condition of the same journey.

The user story is open until two independent Agent products complete the whole
loop without a human copying Event content between them. The exact CLI journey
and competing alternatives are recorded in [evaluations.md](evaluations.md).

## Supporting requirements, not additional stories

- Durable, ordered, repeatable history makes continuation possible.
- Local and remote deployment must run the same journey.
- Remote identity and revocable per-Agent credentials make the journey safe
  outside one trusted machine.
- SQLite and PostgreSQL are deployment choices, not user-visible product
  variants.
- Games, organization policy, browser replay, share pages, snapshots, and
  continuation-room forks are not current product stories.

## The three entities

- **Room** is the durable shared boundary and owns one ordered Event sequence.
- **Actor** is a stable participant identity independent of a process, session,
  model, person, or Agent product.
- **Event** is one immutable contribution appended by an Actor.

There is no fourth core entity. A task, handoff, correction, approval, result,
game phase, or vote is content inside an Event or a projection over Events.

## Who it is for

The primary user is a person or system moving ongoing work between Agent
products or later processes. Humans may inspect or contribute through the same
CLI or HTTP contract, but Tearoom does not require a separate human interface.

## Invariants

- **One Room, one sequence.** Events receive dense increasing sequence numbers
  under one serialization boundary.
- **Append only.** Accepted Events are never edited in place.
- **Stable Actors.** Actor identity survives process, session, model, and
  product changes.
- **Caller-owned read boundaries.** Reading is repeatable. A caller supplies
  its own sequence boundary; Tearoom stores no implicit per-Actor cursor,
  unread marker, or acknowledgement.
- **Open reads.** Reading requires no credential: the Room URL is the read
  capability. Authorization gates writes and Room management, never reads.
- **Ordinary append.** The Store assigns the next position. A normal append
  does not depend on the writer's last read.
- **Idempotent retries.** A caller may attach an idempotency key so an uncertain
  retry returns the original Event.
- **Membership visibility.** Every member reads the complete Room history.
  `to` is public addressing metadata, not secrecy, permission, or a request
  to start a process.
- **One product locally and remotely.** The service, CLI, Room, Actor, Event,
  admission, read, send, errors, migrations, and acceptance journey remain the
  same. Deployment changes infrastructure only.
- **No caller-owned shared rules.** Anything every client must obey is enforced
  below the CLI rather than repeated by each caller.

## One product, two deployment profiles

| Profile | Database | Connection and identity | Current state |
|---|---|---|---|
| Local | SQLite is the only local backend | Nearest local Room or loopback URL; local setup bootstraps its owner with no login ceremony | SQLite ships as the only local backend and the loopback service ships |
| Remote | PostgreSQL | HTTPS Room URL plus login identity and revocable Agent credentials | Not implemented |

These are profiles, not editions. After profile bootstrap, both run the same
four-step `read` / `send` journey. Remote deployment must not add a second
server, API, command vocabulary, Event shape, migration chain, or authorization
model. PostgreSQL is selected by the service, never by an Agent-side
`--store postgres` flag.

Features land in the shared service and are accepted against both profiles.
A feature maintained only in a separate local or remote path is incomplete.
The current loopback-only server is an incomplete deployment of the target
service, not a separate Local Tearoom product. The storage backend is a
deployment fact, never an Agent-side flag.

## Target remote journey — not implemented

The [CLI reference](reference/cli.md#target-remote-profile-not-shipped) owns
the exact target commands:

1. An owner authenticates to the remote service and creates a Room.
2. The owner explicitly admits an Actor and issues that Actor an expiring
   credential.
3. The Agent receives the Room URL, Actor identity, and credential through an
   operator-controlled channel.
4. The Agent uses ordinary `read` and `send`; no remote-only collaboration
   command exists.
5. The owner may revoke that credential without changing the Room, Actor,
   other credentials, or prior Events.

Each Agent or integration receives its own credential. Actor names are not
credentials, and presenting a credential never silently creates membership.
Credentials are opaque random tokens, hashed at rest, printed once, and
distributed over the operator-controlled channel; revocation blocks further
writes without touching history. A hosted GitHub OAuth login may later become
the issuance layer for human owners — a ceremony in front of the same
credential model, never a second authorization model — and is reconsidered
only when a deployment needs identities the operator channel cannot name. The
default expiry remains open until implementation evidence requires a decision.

## Persistence direction

SQLite is the only local backend and PostgreSQL the target remote database:

- `room init` creates the SQLite Room; the storage backend is not a flag. The
  layout is recorded in `.tearoom/store.json`.
- SQLite and PostgreSQL share one relational schema owner and migration
  sequence; PostgreSQL validation is still pending.
- Database models remain behind the Store boundary.
- Transactions enforce dense order, append-only Events, explicit membership,
  visibility, and adopted retry behavior.
- Changing the backend must not change CLI, HTTP, or Event behavior.

The detailed storage boundary is in
[architecture/persistence.md](architecture/persistence.md).

## Delivery and process boundary

`send` durably appends; it does not start or wake another process. An external
operator or Agent product starts the receiving Agent and gives it the Room
connection. The receiver then calls `read`, continues, and calls `send`.

Tearoom owns the shared record, not process scheduling. A later real need may
justify an external runner, but runner state must never become Actor read state
or another core entity.

## What it refuses

- No built-in workflow engine, scheduler, launcher, DAG, or model router.
- No task, lease, gate, thread, channel, game, snapshot, or share-link entity
  in the kernel.
- No share page, browser replay, or management interface in the current target.
- No hidden compatibility model beside Room, Actor, and Event.
- No hidden read checkpoint, unread state, or mandatory stale-write
  precondition.
- No read authorization, credential scopes, or a local login state.
- No claim that `to` is security or process activation.
- No product-owned wake control plane.
- No chat application as a Room; an external channel may carry connection
  details, but its messages never become Events.

## Command surface

The [CLI reference](reference/cli.md) is the sole command contract. The normal
collaboration surface is `read` and `send`. Lifecycle stays under `room`,
the local service stays under `server`, and remote bootstrap adds only login,
Room creation, and credential management.

## Completion standard

The product story is proven only when an executable evaluation shows:

1. Actor A appends through one Agent product.
2. A later Actor B in another Agent product pulls the JSONL history with
   `read`, without manual relay.
3. Actor B appends a result with `send`.
4. Actor A or Actor C pulls that result and can repeat the loop.

The same evaluation must survive a process restart and explicit read boundary.
Foundation checks separately prove dense ordering, membership visibility,
repeatable reads, concurrency, and retry behavior. Remote readiness additionally
requires the same journey on PostgreSQL with authentication and per-Agent
revocation. Proof status belongs only in [evaluations.md](evaluations.md).

## Clean-break policy

No release carries old command aliases, dual Event schemas, cursor-session
imports, private directed visibility, dual HTTP shapes, or deprecated Store
paths. Rollback uses a matched release and data backup rather than a second
compatibility model.
