---
type: product
title: Product definition
description: Authoritative definition of Tearoom's three concepts, the room tree, policy boundaries, and the completion standard.
tags: [product, scope, authority]
timestamp: 2026-11-07
---

# Product definition

This document is the only authority for product scope. The implementation gap
is in [status.md](architecture/status.md), executable proof in
[evaluations.md](evaluations.md), machine-facing behavior under
[reference/](reference/index.md), and dated decisions in [log.md](log.md).
Historical behavior does not redefine this document.

## One sentence

**Tearoom is a long-lived collaborative workspace where people and Agents
write, continue, and deliver work inside Rooms; Rooms form a single-parent
tree, and whoever reads a Room can pick up the work — across Agents, tools,
and process restarts.**

## The kernel: three concepts

| Concept | Sole responsibility |
|---|---|
| **Room** | One span of context. It has a parent (or none) and carries one ordered record stream. |
| **Record** | One entry in that stream: sequence number, time, signature, content. Append-only; never edited. |
| **Notification** | The envelope emitted when a record lands: which room, which sequence, which signature. It stores nothing. |

Three orthogonal axes: Room scopes the range, Record carries the content,
Notification carries the propagation. There is no fourth concept. A task,
handoff, verdict, approval, or status is a sentence inside a record or a
convention over records — never a field.

Words that were merged away: unit/topic/channel are Rooms; parent pointers,
trees, roots, and subtrees are one stored field plus a shape derived at read
time; "current state" is the last N records; closure, archive, and reopen are
sentences people write; verification uses another ordinary Room; "work" and
"project" are informal names for a subtree. Only Room, Record, and Notification
remain.

## Who it is for

The primary user is a person coordinating work that Agents split, advance, and
verify. Humans intervene only at three kinds of moments: set or change a goal,
approve irreversible or outward-facing actions, and handle escalations.
Everything else proceeds by reading Rooms.

## Invariants

- **Single-parent forward tree.** Each Room records its own parent. A parent's
  child list is derived by reading, never cached. Rooms are peers; a root is
  just the Room without a parent.
- **One stream per Room.** Records receive dense increasing sequence numbers
  per Room, assigned by the Store under one serialization boundary. Streams of
  different Rooms are independent.
- **Append only.** Accepted records are never edited in place.
- **No cross-Room writes.** A participant writes only to the Room it is working
  in. Nothing writes into two Rooms at once.
- **Signature is a label.** The author field is an unverified string. Nothing
  checks it, blocks on it, or requires joining to use it.
- **Caller-owned read boundaries.** Reading is repeatable; the caller supplies
  its boundary. Tearoom stores no cursor, unread marker, or acknowledgement.
- **Idempotent retries.** A caller may attach an idempotency key so an
  uncertain retry returns the original record instead of a duplicate.
- **Durable by default.** Context lives in the workspace, not in any session,
  process, or tool transcript.

## Policy, not concepts

These are conventions people and Agents follow; the tools neither represent
nor enforce them:

- A participant writes only to its own Room.
- Quoting writes `room-name#seq` as plain text; tools do not parse it.
- Closure writes a sentence; reopening writes a sentence. There is no status
  field and no state machine.
- The executor never verifies its own work; verification is done by another
  Agent in another ordinary Room, from evidence only.
- Handoffs write three things: what was done, what is blocked, and what the
  next person should do first.
- Which notification wakes whom, and how, is decided entirely outside Tearoom
  (for example by an external scheduler watching `tearoom watch`).

## Command surface

The [CLI reference](reference/cli.md) is the sole command contract. The
surface is: `room init` (with `--parent`), `send`, `read` (a room, a subtree,
a tail), `tree`, and `watch`. A minimal local HTTP face serves the same
contract; see [the HTTP API](reference/http-api.md).

## Notification and waking

An append emits a notification: which room, which sequence, which signature,
when. Tearoom does not decide whether anyone is woken, who, or how — it only
says "there is something new". The `watch` command prints these envelopes; an
external waker (for example a script driving [herdr](https://github.com/williamfzc))
reads the handoff records and starts the next worker. No attention management
ships in the kernel.

## What it refuses

- No membership, admission, credentials, joins, or an actor registry. Reading
  and writing require no permission ceremony inside the trust domain of the
  workspace directory.
- No @-addressing, no per-record recipients, no visibility filtering.
- No multi-parent Rooms, merges, sibling links, leaf cross-links, a global
  index, or a child-list cache.
- No status, closure, or handoff fields; no state machine.
- No hosted control plane, owner tokens, or per-Actor bearer credentials.
  Serving beyond one trusted machine is a future need and would reuse the
  same Store contract.
- No workflow engine, scheduler, or launcher in the kernel; no attention
  management; no hidden compatibility model beside the three concepts.

## Persistence direction

One workspace is one `.tearoom` directory of plain files: one JSONL record
stream per Room, one identity file per Room, one format marker. The layout
is an ordinary git citizen — appends are new lines, new Rooms are new
directories — so the tree's memory can travel with the repository that holds
the work. Details: [architecture/persistence.md](architecture/persistence.md).

## Completion standard

The product is proven only when these three journeys hold with real Agents:

1. **Solo delivery.** One Agent completes a task end to end with zero human
   intervention, reading only its Room.
2. **Build and review.** Two Agents work as builder and reviewer; the reviewer
   reads only the implementation Room and returns pass or fail with evidence;
   the executor cannot certify its own work.
3. **Resume across a day.** A new process, optionally a new tool, reads only
   the Rooms and answers: what is the goal, what did the previous worker do,
   does it meet acceptance, and what is next.

Notification additionally requires a record append to reach an external waker
that starts the next worker with no human relay. Proof status belongs only in
[evaluations.md](evaluations.md).

## Clean-break policy

No release carries command aliases, dual schemas, a compatibility layer, or a
second path beside the single tree model. Storage format changes are one-time
migrations (an old workspace opens once, is upgraded, and the old layout is
never read again); rollback uses a matched release and a data backup rather
than an in-product compatibility mode.
