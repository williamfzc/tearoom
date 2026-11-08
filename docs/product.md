---
type: product
title: Product definition
description: Authoritative definition of Tearoom's room conversation, record stream, and completion standard.
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

## The kernel: two concepts

| Concept | Sole responsibility |
|---|---|
| **Room** | One span of context. It has a parent (or none) and carries one ordered record stream. |
| **Record** | One entry in that stream: sequence number, time, signature, content. Append-only; never edited. |

Two orthogonal axes: Room scopes the conversation, Record carries its
content. A task, handoff, verdict, approval, or status remains content or a
convention over records — never a kernel field. The Room is the shared
conversation: readers of a Room see its same ordered history.

Words that were merged away: unit/topic/channel are Rooms; parent pointers,
trees, roots, and subtrees are one stored field plus a shape derived at read
time; "current state" is the last N records; closure, archive, and reopen are
sentences people write; verification uses a different Agent instance and role
in the same Room; "work" and "project" are informal names for a subtree; the
notification envelope died with the watch surface — observation is a plain
read, and wake-up belongs entirely to the collaboration runtime. Only Room
and Record remain.

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
- **One conversation per Room.** Every collaborator role configured for a Room
  reads and writes the same ordered record stream; roles do not get private
  streams within that Room.

## Conversation and runtime policy

These rules shape conversation and dispatch. Roles and role instances belong to
the collaboration runtime; they do not add kernel concepts or an authorization
boundary:

- A Room may configure multiple collaborator roles. Each role has a reusable
  definition (including its command, working directory, and role instructions)
  and a Room-scoped runtime instance bound to an hc panel. Reusing a definition
  in another Room creates a separate instance there.
- Room messages are shared by all configured roles. A message may be a
  broadcast or address one or more roles with `@`; addressing asks the runtime
  to route the message through hc and does not hide the record from other
  readers or grant access. Recipients live in the record's structured content,
  never in the kernel Record itself.
- The CLI exposes the collaborators configured for a Room so an Agent can
  inspect roles and decide whom to involve. This roster is coordination
  information, not admission: it neither authorizes reads/writes nor validates
  a record signature.
- Quoting writes `room-name#seq` as plain text; tools do not parse it.
- Closure writes a sentence; reopening writes a sentence. There is no status
  field and no state machine.
- The executor never verifies its own work; a different Agent instance using
  a reviewer role reviews the implementation in the same Room and records its
  evidence-based verdict in that shared stream.
- Roles working in different Rooms coordinate through a shared ancestor Room.
  Cross-Room handoffs and decisions are recorded there; the Room tree remains
  the scope and history boundary.
- Handoffs write three things: what was done, what is blocked, and what the
  next person should do first.
- Which wake happens, and how, is decided by the collaboration runtime: a
  broadcast starts no panel, and an `@role` is routed to that role's
  Room-scoped panel.

## Command surface

The CLI must configure collaborators when creating a Room and show the
configured collaborators so an Agent can decide whom to involve. The [CLI
reference](reference/cli.md) documents the shipped surface: `room init` (with
`--parent`), `send`, `read` (a room, a subtree, a tail), `tree`, and the
`role`/`collab` collaboration commands.

## What it refuses

- No permission-based membership, admission, credentials, or join ceremony.
  Room collaborator roles are a coordination roster only; anyone within the
  workspace trust domain may read and write without being added to it.
- No per-record visibility filtering. Addressed messages do not make Room
  records private.
- No multi-parent Rooms, merges, sibling links, leaf cross-links, a global
  index, or a child-list cache.
- No status, closure, or handoff fields; no state machine.
- No hosted control plane, owner tokens, or per-Actor bearer credentials.
  Serving beyond one trusted machine is a future need and would reuse the
  same Store contract.
- No workflow engine, scheduler, or launcher in the Tearoom kernel; no hidden
  compatibility model beside the two concepts. Collaboration routing sits
  outside the kernel and starts from the record history itself.

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
2. **Build and review.** Separate Agent instances use builder and reviewer
   roles in the same Room; the reviewer evaluates the shared record history
   and returns pass or fail with evidence. The executor cannot certify its own
   work.
3. **Resume across a day.** A new process, optionally a new tool, reads only
   the Rooms and answers: what is the goal, what did the previous worker do,
   does it meet acceptance, and what is next.

Delivery additionally requires addressed Room messages to reach the intended
role through the `hc`-based runtime without a human relay. Reliable recovery
and panel-failure behavior remain to be designed and proven. User stories and
proof status belong in
[domain/user-stories.md](domain/user-stories.md) and
[evaluations.md](evaluations.md), respectively.

## Clean-break policy

No release carries command aliases, dual schemas, a compatibility layer, or a
second path beside the single tree model. Retired formats are refused, not
migrated: an old workspace is converted once by an operator with the release
that still reads it, and the old layout is never read again. Rollback uses a
matched release and a data backup rather than an in-product compatibility
mode.
