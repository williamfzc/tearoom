---
type: domain
title: Room, Actor, Event
description: Canonical definitions of Tearoom's three domain entities and their invariants.
resource: internal/core/core.go
tags: [domain, concepts, invariants]
timestamp: 2026-08-28
---

# Room, Actor, Event

The product has exactly three domain entities. Their authoritative scope is in [the product definition](../product.md); `internal/core` is the canonical implementation.

## Room

A Room is a durable, named boundary containing actors and one ordered event stream. Local mode binds one Room to a `.tearoom` directory. Shared mode exposes that same Room over HTTP.

A Room is deliberately more general than a task, thread, channel, repository, or game. Those are interpretations of its events.

## Actor

An Actor is a stable participant identity with an optional role label. Identity comes from `TEAROOM_ACTOR` or `--as`, not from the process, so a later session can resume the same participant. Event operations register that identity automatically.

An Actor may be a human, a coding agent, another agent product, or a game role controller. The core does not distinguish them.

## Event

An Event is one immutable fact in the Room log. It carries:

- a dense room sequence and event id;
- the actor that appended it;
- a type and optional body or JSON data;
- optional recipients;
- an optional causing event;
- an idempotency key.

Empty recipients mean public. Otherwise only the sender and named recipients receive the event from `Read`.

## The write contract

Every append supplies `Expected`, the cursor returned by the read on which the event is based.

```text
read -> cursor N
reason or act
append(expected=N)
```

If the Room has advanced to `N+1`, the append is rejected. The caller must read again and decide whether its event is still true. This turns stale context into an explicit conflict rather than a recorded falsehood.

The idempotency key answers a different question: whether the same intended event has already landed. The same key and content return the original Event; the same key with different content is a conflict.

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
