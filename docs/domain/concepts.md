---
type: domain
title: Domain concepts
description: The three concepts — Room, Record, Notification — and how shared Room conversation relates to runtime collaboration.
resource: docs/product.md
tags: [domain, concepts, invariants]
timestamp: 2026-11-07
---

# Domain concepts

The product has exactly three domain concepts. [product.md](../product.md)
owns the product facts and the invariants; this page explains them and their
boundary behaviour in one place. Implementation gaps belong only in
[status.md](../architecture/status.md); machine-facing contracts are in
[reference/](../reference/index.md).

## The three concepts

### Room

A Room is one span of context. It has a parent Room (or none, which makes it a
root) and carries one ordered record stream. Rooms form a single-parent tree:
each Room stores only its own parent, and a parent's child list is derived by
reading at query time. Rooms are peers — a root is just the Room without a
parent, and a "project" is an informal name a person gives to some subtree.
Rooms are identified by name within their workspace.

A Room is also a shared conversation: broadcast and addressed messages are
visible in the same Room history. Addressing guides runtime delivery; it is not
privacy or permission. See [the product contract](../product.md) for the
current boundary and [open questions](open-questions.md) for unresolved
delivery semantics.

A Room can configure several collaboration roles over this one stream. Role
definitions can be reused, while each Room gets its own runtime instances and
hc panels. Roles are runtime configuration, not a fourth kernel concept.

### Record

A Record is one immutable entry in a Room's stream:

- `seq`, the dense position inside its Room and its replay address;
- `author`, the signature — an unverified label;
- `at`, assigned when the record is accepted;
- `content`, arbitrary valid JSON — plain text sent through the CLI is encoded
  as a JSON string.

Every Room owns its own stream; the streams of different Rooms are
independent, and a child Room's first record is `seq` 1 regardless of its
parent's head. Append-only: accepted records are never edited.

### Notification

A Notification is the envelope emitted when a record lands: which room, which
sequence, which signature, when. It stores nothing and carries no recipient.
The collaboration runtime uses the Room record and its addressing convention
to route messages to role panels. `hc` is the intended Agent runtime; it does
not itself define Tearoom's Room-to-role configuration or routing policy.

## Write and read contract

An append requires no identity, no admission, and no saved cursor; the Store
assigns the next dense sequence of the target Room under its serialization
boundary. `read` is a pure, repeatable query whose `--after` boundary belongs
to the caller and is never saved; polling for later records is a caller loop,
not a subscription. A read boundary is not proof that anyone read,
understood, or completed anything.

```text
tearoom read --room implementation --after e42
reason or act
tearoom send --text "..." --as codex --room implementation
```

A participant writes only to the Room it works in; nothing ever writes into
two Rooms at once. An idempotency key answers whether the same intended
record already landed; it is scoped per (room, signature, key) and is
independent of read progress.

## Boundary behaviour

| Situation | Behaviour |
|---|---|
| Writers race in one Room | Store serialization assigns distinct dense sequence numbers |
| Writers race in different Rooms | Streams are independent; neither waits on the other |
| Writer crashes mid-append | Transaction semantics release the lock on recovery; no partial record is accepted |
| Identical request is retried | An idempotent append returns the original record |
| A child Room is created | It records its parent once; the parent's data is never written to |
| The child list is needed | Derived by reading the workspace at query time; never cached |
| Reader supplies a boundary | Read repeats from the caller's explicit boundary |
| A role is addressed | The runtime routes the shared Room message to that Room's panel for the role |
| Two roles review work | They use the same Room stream; the reviewing role differs from the executing role |
| Work crosses Rooms | The roles coordinate through a shared ancestor Room |
| A role panel fails | The Room preserves history; recovery behavior is not yet defined |

**Platform rule:** the storage backend must provide a real cross-process
serialization primitive or reject concurrent operation on that platform.

## Trust and security

- **Local boundary:** a workspace is a `.tearoom` directory readable by one OS
  user. That directory is the whole trust domain: inside it, anyone may read
  any Room, write any Room, and claim any signature. Record bodies are stored
  exactly as submitted; do not put secrets in records unless the directory is
  protected accordingly.
- **Signature is a label.** The author field is never verified, never
  blocked, and never requires registration. Two tools using the same
  signature produce indistinguishable records by design.
- **Append-only:** the public Store contract has no edit or delete. The local
  owner can still alter files; tamper evidence is not implemented.
- **Before hostile or shared use:** transport security, at-rest protection,
  and abuse controls would be required; none ships today. Serving beyond one
  trusted machine is out of scope (see [product.md](../product.md)).

## The failures the tree prevents

Without a neutral durable workspace, a human becomes the message bus between
terminals and tools:

| Failure | The tree's answer |
|---|---|
| Broken handoff — a process ends and its state disappears | Records live in the workspace, not the session |
| Context scattered across sessions nobody can reassemble | One stream per Room, ordered and durable |
| The human relays context between Agents | Each Agent reads the Rooms it needs and continues |
| Ambiguous scope — who owns what | One parent per Room; the tree names the ranges |
| Executor certifies its own work | A different Agent instance reviews in the same Room, from evidence |
| Duplicate effects from retries | Idempotent append capability |

## What the model does not promise

No permission-based membership, admission, or required join; configured Room
roles are only a coordination roster. There is no unread state or acknowledgements, no
kernel-owned subscriptions, no scheduled wake-ups in the kernel, no per-record
privacy inside one Room, no exactly-once external execution. Address syntax,
broadcast wake policy, and collaborator roster details remain runtime
protocol concerns, not new kernel concepts. The design rule: the cheapest
observation must stay useful — a feature that forces every reader to read more
or run a process must prove the extra cost necessary.

## Related

- [product.md](../product.md) — the authoritative product facts
- [open-questions.md](open-questions.md) — what is deliberately unsettled
- [the HTTP contract](../reference/http-api.md) and
  [the storage layout](../reference/storage-layout.md)
