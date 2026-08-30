---
type: domain
title: Room, Actor, Event
description: Canonical target definitions of Tearoom's three domain entities and their invariants.
resource: docs/architecture/model-discussion.md
tags: [domain, concepts, invariants]
timestamp: 2026-08-30
---

# Room, Actor, Event

The product has exactly three minimum domain entities. Their target scope is in
[the product definition](../product.md) and
[the model rationale](../architecture/model-discussion.md). Implementation
gaps belong only in [status.md](../architecture/status.md).

## Room

A Room is a durable, named boundary containing actors and one ordered event stream. Local mode binds one Room to a `.tearoom` directory. Shared mode exposes that same Room over HTTP.

A Room is deliberately more general than a task, thread, channel, repository, or game. Those are interpretations of its events.

## Actor

An Actor is a stable participant identity. Identity
comes from `TEAROOM_ACTOR` or `--as`, not from the process, so a later session
can resume the same participant. Admission and membership must be explicit for
a real privacy boundary.

An Actor may be a human, a coding agent, another agent product, or a game role controller. The core does not distinguish them.

## Event

An Event is one immutable fact in the Room log. The minimum target shape is:

- `seq`, its dense position and precise replay address;
- `actor`, the admitted stable author;
- `at`, assigned when the Event is accepted;
- optional `to`, naming Room members for attention or routing;
- `content`, whose exact text/JSON shape remains open.

Every Room member can read the complete history. `to` is visible addressing,
not a visibility filter, secrecy mechanism, permission grant, or wake request.
Privacy requires a separate Room. A protocol may wrap `content` with `type`,
`caused_by`, or an idempotency key, but those are not minimum kernel fields.

## The write contract

An ordinary append does not require `Expected`, a saved cursor, or proof of the
writer's last read. The Store assigns the next dense sequence under its
serialization boundary.

```text
read --after e42
reason or act
send <content>
```

Some protocols may need an explicit conditional append such as “accept this only
if the Room head is still e42.” That is a separate capability and remains open;
it is not the default meaning of `send`.

An idempotency key, when an append API exposes one, answers whether the same
intended Event has already landed. It is independent of read boundaries and is
not part of the minimum Event value.

## Application protocols

Examples only; they add no domain entities:

```text
handoff.requested
handoff.accepted
task.claimed
approval.requested
approval.decided
role.assigned
phase.changed
vote.cast
message
```

Protocol rules belong in a protocol-specific layer or client until more than one caller proves they must become a shared store invariant.
