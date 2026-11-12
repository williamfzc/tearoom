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

Role is the collaboration entity above the kernel: a reusable launch
definition and Room assignment, not a Room or Record field. Earlier naming
and retired interfaces are recorded in [log.md](log.md).

## Who it is for

The primary user is a person coordinating work that Agents split, advance, and
verify. The goal is to reduce manual handoffs while keeping it easy for the
person to set direction, review progress, make decisions, and intervene.

## Everyday use

One common path is to set up reusable roles when needed, create a Room for the work, and give it a
goal. A worker records progress there; a separate reviewer can use the same
conversation. Read the Room to continue later, and use Herdr when available to
inspect running collaborators. Rooms can be organized under a parent when work needs
that structure. Role definitions and Room assignments can also be changed
after creation; preparation is not a fixed phase.

The product should make this path understandable without requiring users to
learn delivery recovery, storage details, or every available tree operation.
A shipped operation is not automatically a required part of the main journey.

## Concept budget

The product has three entities: Room, Record, and Role. A new need extends one
of them, or is a view over them; it does not add a fourth entity. A new noun is
a product decision that must say what it replaces or why the existing three
cannot carry it. Launch details and integration state are fields, not peers.

## Invariants

- **Single-parent forward tree.** Each Room records its own parent. A parent's
  child list is derived by reading, never cached. Cycles are refused. Rooms are
  peers; a root is just the Room without a parent.
- **One stream per Room.** Records receive dense increasing sequence numbers
  per Room, assigned by the Store under one serialization boundary. Streams of
  different Rooms are independent.
- **Append only.** Accepted records are never edited in place.
- **No cross-Room writes.** A participant writes only to the Room it is working
  in. Nothing writes into two Rooms at once.
- **Signature is a label.** The author field is an unverified string. Nothing
  checks it, blocks on it, or requires joining to use it.
- **Caller-owned read boundaries.** Reading is repeatable; the caller supplies
  its boundary. The Room store keeps no reader cursor, unread marker, or
  acknowledgement; the delivery runtime tracks its own progress separately.
- **Idempotent retries.** A caller may attach an idempotency key so an
  uncertain retry returns the original record instead of a duplicate.
- **Durable by default.** Shared Room history lives in the workspace and does
  not depend on any session, process, or tool transcript. An Agent's own
  conversation and context remain with its tool.
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
- A Room-role instance continues its existing Agent conversation across wakes;
  the first wake creates one when none exists. A busy instance queues addressed
  messages. A panel is a view and transport for that conversation, not proof of
  its continuity. Recovery and intentional replacement must not silently start
  a fresh conversation as though it continued the old one. The runtime contract
  and limitations are in [collaboration delivery](architecture/collaboration.md).
- Tearoom routes addressed messages, queues delivery, and manages role
  lifecycles. It does not decide how an Agent interprets work, manages or
  compresses its context, or reaches a verdict. On a wake, deliver the new
  Room messages directly; the Agent may read Room history when it needs to.
  Delivery is not proof that the Agent understood or completed the work.
- Local collaboration uses short-lived CLI commands and one background
  service per user for active workspaces. An addressed message written in the
  CLI or web view starts the service after its Room record is saved; reads and
  Room creation do not.
  Closing a command or optional view leaves work running. `service stop`
  attempts to end the service's managed Agent terminals. Its implementation
  and limits are in
  [collaboration delivery](architecture/collaboration.md#local-collaboration-service);
  [implementation status](architecture/status.md) records what has been
  verified.
- Each Room should open in its own named Herdr session. Roles assigned to that
  Room appear as tabs within the session, so viewing one Room does not mix its
  collaborators with other Rooms. Herdr is an optional view: Room history
  remains usable if it is unavailable. The intended mapping and current
  implementation state are in the
  [integration guide](reference/agent-integration.md#herdr-session-per-room).
- Room messages are shared by all configured roles. A message may be a
  broadcast or explicitly address one or more roles; addressing asks the runtime
  to route the message through hc and does not hide the record from other
  readers or grant access. Recipients live in the record's structured content,
  never in the kernel Record itself.
- The CLI exposes the collaborators configured for a Room so an Agent can
  inspect roles and decide whom to involve. This roster is coordination
  information, not admission: it neither authorizes reads/writes nor validates
  a record signature.
- Plain-text citations such as `room-name#seq` are not interpreted as delivery
  instructions; the CLI also supports an explicit reply reference.
- Closure writes a sentence; reopening writes a sentence. There is no status
  field and no state machine.
- When independent review is needed, the executor can check its own work but
  cannot provide that review; a different Agent instance records an
  evidence-based verdict in the same Room.
- When work spans Rooms, a shared ancestor Room can hold the decisions and
  handoffs needed by both branches. The Room tree remains the scope and history
  boundary; the [cross-Room story](domain/user-stories.md#s9-separate-rooms-coordinate-through-their-common-parent)
  tests this arrangement.
- A handoff should make progress, blockers, and the next step clear.
- Which wake happens, and how, is decided by the collaboration runtime: a
  broadcast starts no panel, and an explicit recipient is routed to that role's
  Room-scoped panel.

## Command surface

The CLI must support configuring and discovering collaborators, creating a
Room, sending and reading messages, and seeing the Room structure. An optional
local browser face is a view over the same Store and message rules: it
navigates Rooms, reads history, and appends messages, and it binds only to the
loopback address. It adds no entity, identity, or remote-serving boundary. The
[CLI reference](reference/cli.md) documents what ships, including lower-frequency
operations; inclusion there does not make an operation essential to the main
journey.

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
  compatibility model beside Room and Record. Collaboration routing sits
  outside the kernel and starts from the record history itself.

## Persistence direction

Room history lives in a local workspace and can outlive an Agent session.
The file layout and its portability limits are in
[architecture/persistence.md](architecture/persistence.md).

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
