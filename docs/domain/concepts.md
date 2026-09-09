---
type: domain
title: Domain concepts
description: The three entities, their write/read contract, boundary behaviour, trust model, and the failures the Room prevents — the domain page the other docs link to.
resource: docs/product.md
tags: [domain, concepts, invariants, security]
timestamp: 2026-09-10
---

# Domain concepts

The product has exactly three domain entities. [product.md](../product.md)
owns the product facts and the invariants; this page explains them and their
boundary behaviour in one place. Implementation gaps belong only in
[status.md](../architecture/status.md); machine-facing contracts are in
[reference/](../reference/index.md).

## The three entities

### Room

A Room is a durable, named boundary containing actors and one ordered event
stream. Local mode binds one Room to a `.tearoom` directory; `server start`
exposes that same Room over loopback HTTP. A Room is deliberately more general
than a task, thread, channel, or repository — those are interpretations
of its events.

### Actor

An Actor is a stable participant identity. Identity comes from
`TEAROOM_ACTOR` or `--as`, not from the process, so a later session can resume
the same participant. Admission is explicit; an Actor may be a human, a coding
agent, or another agent product — the core does not
distinguish them.

### Event

An Event is one immutable fact in the Room log:

- `seq`, its dense position and precise replay address;
- `actor`, the admitted stable author;
- `at`, assigned when the Event is accepted;
- optional `to`, naming Room members for attention or routing;
- `content`, arbitrary valid JSON — plain text sent through the CLI is encoded
  as a JSON string.

The invariant, stated once here and linked from everywhere else: **membership
is the visibility boundary — every member reads the complete history; `to` is
visible addressing, not a filter, not secrecy, not permission, not a wake
request.** Privacy requires a separate Room with separate admission. A
protocol may wrap `content` with `type`, `caused_by`, or idempotency keys;
those are not kernel fields. Wire and storage details:
[the HTTP contract](../reference/http-api.md) and
[the storage layout](../reference/storage-layout.md).

## Write and read contract

An ordinary append requires no saved cursor and no proof of the writer's last
read; the Store assigns the next dense sequence under its serialization
boundary. `read` is a pure, repeatable query whose `--after` boundary belongs
to the caller and is never saved by the Room; polling for later Events is a
caller loop, not a subscription. A read boundary is not proof
that an Actor read, understood, or completed an Event.

```text
read --after e42
reason or act
send <content>
```

An idempotency key answers whether the same intended Event already landed; it
is independent of read progress. An explicit conditional append ("accept only
if the head is still e42") remains an open capability, not the default
meaning of `send`.

## Boundary behaviour

| Situation | Behaviour |
|---|---|
| Room changes after an Actor read | Ordinary append remains valid; a protocol may request an explicit conditional append |
| Identical request is retried | An idempotent append capability may return the original Event |
| Writers race | Store serialization assigns distinct dense sequence numbers |
| Writer crashes mid-append | SQLite transaction semantics release the lock on recovery; no partial Event is accepted |
| Store or server is unavailable | Command fails; no cached success is claimed |
| Reader supplies a boundary | Read repeats from the caller's explicit boundary |
| An addressed Agent is offline | Room alone does not wake it; an external operator starts and configures the process, which then uses CLI `read` and `send` |

`seq` identifies an Event in shared history; it is never a per-Actor read
checkpoint. A future durable consumer must own an explicit identity and ledger
rather than borrowing Actor read state.

**Platform rule:** the storage backend must provide a real cross-process
serialization primitive or reject concurrent operation on that platform.

## Trust and security

- **Local boundary:** a local Room is a directory readable by one OS user.
  Event bodies are stored exactly as submitted; do not put secrets in Events
  unless the directory is protected accordingly. The loopback HTTP server must
  never be exposed to an untrusted network.
- **Append-only:** the public Store contract has no edit or delete. The local
  owner can still alter files; tamper evidence and signed export are not
  implemented.
- **Delivery:** an addressed Event grants no execution authority. Delivery to
  an absent Agent was adjudicated out of product scope on 2026-09-09 — a
  sixty-line external rival matched the in-product design
  ([lessons/rival-falsification.md](../lessons/rival-falsification.md)).
  Anything that delivers must never present its state as Actor read,
  comprehension, or task completion.
- **Deployment identity:** Tearoom has one authorization model. Local setup may
  bootstrap its owner from the trusted machine; remote setup uses a user login
  and separate revocable per-Agent credentials. Actor names are never secrets,
  and presenting a credential never silently creates membership. This shared
  path is not implemented yet; see [status.md](../architecture/status.md).
- **Before hostile or remote use:** transport security (TLS in front),
  at-rest protection, rate limits, abuse controls, and audit records for
  management changes are required.

## The failures the Room prevents

Without a neutral durable room, a human becomes the message bus between
terminals and tools:

| Failure | The Room's answer |
|---|---|
| Broken handoff — a process ends and its state disappears | Stable Actors and durable Events |
| Ambiguous context — an actor acts on a stale view | Ordered history with caller-owned boundaries; freshness guarantees are opt-in |
| Duplicate effects — retries create the same effect twice | Idempotent append capability |
| Lost or ambiguous order under concurrency | Store-serialized dense sequence assignment |
| Unbounded or wrongly separated context | Caller-chosen boundaries; privacy by separate Room membership |
| Offline continuation — the addressee is absent | The Room preserves the history; an externally started process pulls it through CLI `read` and writes back through `send` |

## What the target does not promise

No server-owned unread state or acknowledgements; no mute, subscription, or
mention tiers; no scheduled wake-ups in the kernel; no automatic summaries or
long-term Actor memory; no private Event bodies inside one Room; no
exactly-once Runtime execution. These may become application protocols or
control-plane capabilities; none adds a fourth entity. The design rule: the
cheapest observation must stay useful — a feature that forces every Actor to
read more or run a process must prove the extra cost necessary.

## Application protocols

Protocol rules belong in a protocol-specific layer or client until more than
one caller proves they must become a shared Store invariant. The one target
journey is [harness-workflow.md](harness-workflow.md); genuinely unsettled
questions are listed in [open-questions.md](open-questions.md).
