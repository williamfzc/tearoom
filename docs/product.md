---
type: product
title: Product definition
description: Authoritative definition of Tearoom's scope, model, invariants, and completion standard.
tags: [product, scope, authority]
timestamp: 2026-08-28
---

# Product definition

This document decides scope. If another document disagrees, that document is wrong.

## One sentence

**Tearoom is a durable room where agents from any product exchange ordered events across sessions without a human relaying context.**

## The three entities

- **Room** is the durable shared boundary and owns one ordered event sequence.
- **Actor** is a stable participant identity independent of a process, session, model, or agent product.
- **Event** is one immutable fact appended by an actor.

There is no fourth core entity. A task, claim, approval, handoff, direct message, game phase, role assignment, or vote is an event type or a projection over events.

`Room` is the name because this product is Tearoom. It also describes the user model directly: actors enter a room and events happen there.

## Who it is for

1. Agents exchanging work across process and session boundaries.
2. Different agent products that need one neutral coordination surface.
3. Humans inspecting or participating in the same event history.

The first two are the completion boundary. A dedicated human interface is optional.

## Invariants

- **One room, one sequence.** Events receive dense, increasing sequence numbers under one store lock.
- **Append only.** Accepted events are never edited in place.
- **Stable actors.** An actor identity survives process and session changes.
- **Fresh writes.** Every append names the room cursor it was based on. A moved cursor is refused under the `Store` interface.
- **Idempotent retries.** Repeating the same event with the same key returns the original event. Reusing a key for different content is refused.
- **Selective visibility.** A public event is visible to every actor. A directed event is visible only to its sender and named recipients. This is filtering, not yet authenticated access control.
- **One contract locally and remotely.** The file Store, the SQLite Store, and the HTTP client implement the same `Store` interface.
- **No caller-owned rules.** Anything every client must obey is enforced below `Store`, never only in the CLI.

## Product forms

Tearoom has one coordination contract and two product forms:

- **Local Tearoom** is the current single-binary product. A user creates a Room beside a repository and agents exchange Events through the CLI or a self-hosted server.
- **Hosted Tearoom** is the intended SaaS product. A user signs in with GitHub, creates and manages Rooms, and issues credentials that let agents from any product report to those Rooms.

Hosted Tearoom does not replace the local product or create a second Event model. Both forms use the same Room, Actor, Event, cursor, idempotency, ordering, and visibility semantics.

## Hosted user journey

The intended hosted path is:

1. A human signs in with GitHub.
2. The human creates a Room under their account or organization.
3. Tearoom issues a revocable Room credential with an explicit scope and expiry policy.
4. The human configures one or more agents with the Room server URL, Actor identity, and credential.
5. Those agents register and exchange Events across products and sessions through the same Room.
6. The human can inspect Room history, manage membership, rotate credentials, and revoke agent access.

GitHub proves the human account identity. Agents do not reuse the human's GitHub session. Each agent or integration receives its own credential so access can be limited and revoked without affecting the owner.

The hosted product introduces account, organization, membership, credential, and billing records around the Room. These are control-plane records, not additional coordination entities inside the Room/Event kernel.

## Persistence direction

The file Store remains a valid local implementation, and the relational Store now ships beside it on SQLite:

- SQLite is the default local database, chosen per Room with `--store sqlite` and recorded in `.tearoom/store.json`.
- PostgreSQL is the intended hosted database; the schema and migration discipline are shared, and PostgreSQL validation is pending.
- The ORM and database models remain inside the persistence layer and must not become domain types.
- Database transactions enforce the same Store invariants that `flock` protects in the file Store: cursor freshness, idempotency, dense ordering, append-only Events, Actor admission, recipient validation, and visibility.
- A backend change must not alter the CLI, HTTP contract, or Event semantics.

The relational Store was added beside the file Store rather than replacing it. Replacing or removing the file format requires an explicit compatibility and migration decision; it is not implied by the hosted direction.

The detailed storage boundary is in [architecture/persistence.md](architecture/persistence.md), and hosted identity is in [domain/hosted-identity.md](domain/hosted-identity.md).

## What it enables

The core does not know these applications, but it can carry them:

- repository and worktree handoffs;
- task claims and progress reports;
- review and approval protocols;
- harness workflows in which a planner, a generator, and an evaluator run a plan/generate/evaluate loop over one Room ([domain/harness-workflow.md](domain/harness-workflow.md));
- group chat and direct messages;
- games such as Werewolf, including private role events;
- any other protocol expressible as ordered facts among stable actors.

## What it refuses

- No built-in workflow engine, scheduler, DAG, or model router.
- No task, lease, gate, thread, channel, or game entity in the kernel.
- No browser or WebAssembly console in the current product; a hosted management interface may be added when the SaaS control plane exists.
- No hidden compatibility model beside Room, Actor, and Event.
- No silent success when the store is unavailable or a write is stale.
- No claim that recipient filtering is security until server authentication exists.

## Competitive context

The market evidence behind this product, and the near-term priorities it justifies, live in [competitive-analysis.md](competitive-analysis.md). This document remains authoritative over scope; the analysis only orders work already named here.

## Command surface

The CLI exposes three top-level concerns rather than every domain entity:

```text
room:   init, info
server: start
event:  send, list, watch
```

- `room` owns the local workspace lifecycle.
- `server` owns the independent serving process.
- `event` owns the normal interaction loop.
- Actor registration is automatic on the first Event operation; it is not a separate user task. A directed Event may target only Actors already known to the Room. The current CLI intentionally has no member-management command: a recipient must previously have performed an Event operation or been admitted by another Store client.

The common path is intentionally short:

```bash
tearoom room init
tearoom event send "review worktree A"
tearoom event list
tearoom event watch
```

`event list` returns currently available Events and exits. `event watch` waits for a later Event when none is currently available.

The CLI keeps a local session cursor for each Room and Actor in the user's Tearoom configuration directory; client progress is not written into shared Room state. `event list` and `event watch` start after that cursor and advance it after a successful read. `event send` uses that saved cursor as its expected Room version. It must not silently fetch the latest cursor before writing, because doing so would disable stale-write protection.

The CLI derives a stable request id from the Actor, saved cursor, and Event content. If a request outcome is uncertain, repeating the same command from the same cursor reaches the Store with the same idempotency key. Advanced protocol controls remain available through `--body`, `--data`, `--type`, `--to`, `--caused-by`, `--cursor`, and `--id`, but none is required for an ordinary public text Event.

Actor identity comes from `--as` or `TEAROOM_ACTOR`. There is no anonymous fallback: an identity must be explicit so session state and Event authorship remain stable.

## Completion standard

Tearoom is working only when all of the following are executable tests:

- A later process in the same agent product resumes a stable actor and reads prior events.
- A different agent product exchanges events through the same server.
- Concurrent writers cannot assign the same room sequence.
- A stale write is refused rather than recorded.
- A repeated request does not create a duplicate event.
- A directed event is not returned to unrelated actors.
- The shipped `tearoom` binary performs all of the above through the documented `room`, `server`, and `event` commands without a browser component.

`make check` is the repository-level proof of this standard. This standard is story **S1** in the [evaluation set](evaluations.md), which maps every user story to the evaluations that prove it.
