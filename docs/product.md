---
type: product
title: Product definition
description: Authoritative definition of Tearoom's scope, target model, implementation status, and completion standard.
tags: [product, scope, authority]
timestamp: 2026-09-09
---

# Product definition

This document is the only authority for product scope. The code gap is in
[status.md](architecture/status.md), executable proof in
[evaluations.md](evaluations.md), target interfaces under
[reference/](reference/index.md), and dated decisions in
[log.md](log.md). Historical behavior does not redefine this document.

## One sentence

**Tearoom is a durable shared Room where people, Agents, and tools contribute to one ordered, replayable history; a context link carries that history anywhere — a human opens the replay, and an absent Agent reads it and continues.**

## The three entities

- **Room** is the durable shared boundary and owns one ordered event sequence.
- **Actor** is a stable participant identity independent of a process, session, model, or agent product.
- **Event** is one immutable fact appended by an actor.

There is no fourth core entity. A task, claim, approval, handoff, game phase,
role assignment, or vote is a content convention or a projection over Events.

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
- **Caller-owned read boundaries.** Reading is a repeatable query. A caller may provide an explicit sequence boundary; Tearoom does not persist an implicit per-Actor read cursor, unread marker, or acknowledgement.
- **Ordinary append.** The Store assigns the next dense sequence. A normal append does not require the writer to prove which read cursor it used. An explicit conditional append may be added when a protocol proves it needs one.
- **Idempotent retries.** An append API may use an idempotency key so an uncertain retry returns the original Event; this is an append capability, not part of the minimum Event shape.
- **Membership visibility.** Every member of a Room can read its complete history. `to` is public addressing or attention metadata, not secrecy, permission, or a process-control request. Privacy requires a separate Room with separate admission.
- **One product locally and remotely.** Deployment changes infrastructure, not
  product semantics. The same service, Room, Actor, Event, admission, send,
  read, errors, migrations, and acceptance journeys apply everywhere.
- **No caller-owned rules.** Anything every client must obey is enforced below `Store`, never only in the CLI.

## Adopted contract

The collaboration kernel is intentionally small:

| Area | Product contract |
|---|---|
| Read progress | Caller-supplied `read` boundaries; no implicit Actor cursor |
| Visibility | Room membership exposes complete history; `to` is public addressing |
| Event minimum | `seq`, `actor`, `at`, optional `to`, and `content` |
| Append freshness | Ordinary append is independent of read progress; conditional append is explicit if later proven |
| Delivery | A paste, a context link, or an operator-owned external script; no in-product wake control plane (adjudicated 2026-09-09) |

The implementation may lag this contract, but no public or internal interface
may treat the older model as a second supported product.

## Clean-break migration policy

The pre-adoption model has no compatibility guarantee. The migration must not
retain command aliases, dual Event schemas, cursor session import, directed
visibility fallback, dual HTTP shapes, or a deprecated Store path. Code, data
schemas, tests, demos, skills, and documentation switch together when a phase
lands.

Old experimental Rooms and remote coordination data may be reset. If any data
is worth keeping, export it before the cutover; do not add runtime compatibility
machinery to preserve it. Rollback means reverting the release and its data
snapshot, not carrying the old model forward inside the product.

## One product, two deployment profiles

Tearoom is one product and one service, deployed with different infrastructure:

| Profile | Database | Network and identity | Current state |
|---|---|---|---|
| Local | SQLite is the target default; the file backend remains a current portable option | Loopback; local setup may bootstrap its owner | The current single-binary product ships, with file as its default and SQLite optional |
| Remote | PostgreSQL | HTTPS; login identity and scoped, revocable Agent credentials | Not implemented |

These are profiles, not editions. Remote deployment must not introduce a
second server, API, command vocabulary, Event shape, migration chain, or
authorization model. Local setup may remove ceremony — for example by
bootstrapping one owner — but requests then pass through the same admission and
capability decisions as remote requests. Infrastructure adapters such as the
database, object storage, TLS termination, and process supervision are the
allowed differences.

Features land in the shared service first and are accepted against both
profiles. A feature that exists only in a separately maintained local or remote
path is incomplete. PostgreSQL parity is required before claiming remote
readiness; SQLite remains the always-runnable local proof of the same relational
contract. The current loopback-only server is therefore an incomplete deployment
of the target service, not a separate Local Tearoom product.

The first remote-facing slice is the **context link** (below). It grows from the
same service and Room history; it is not a separate remote application.

## The context link

A context link turns a caller-chosen Room range into one pasteable URL with
two faces: a human opens it and sees the replay — actors, order, events — and
an agent fetches it and reads the same history as text or JSONL with no
Tearoom install. The link is a caller-owned read boundary plus transport, the
same boundary `read --after` already takes. It is a projection, not a fourth
kernel entity, and it carries a reference to Events, never a second copy of
collaboration truth.

The slice grows in three tiers:

1. **Snapshot link.** A content-addressed, immutable export of one Room
   range, resolved from object storage. Publishing is explicit: creating a
   link releases that range from the Room's membership boundary to whoever
   holds the URL — the same act as pasting into a chat.
2. **Continuation room.** The link gains a write face: a scoped, revocable
   append credential to a remotely deployed Room created from the snapshot, under the
   same Room/Actor/Event contract. Guest admission is explicit, and a shared
   capability names but does not prove its Actor — identity is weaker than a
   locally admitted Actor, and the link view must say so.
3. **Remote deployment.** The login-identity journey below, with
   organizations, credentials, and revocation.

Collaboration continues across a link by a fork chain: import the snapshot
into a Room, work, and share a new link. A chat thread carrying that sequence
of links is a transport, not a Room: chat messages never become Events, and no
chat application becomes a coordination surface of this product. The context
budget stays with the receiver — the link encodes its boundary, and full-range
reads cost what [lessons/rival-falsification.md](lessons/rival-falsification.md)
measured (about 93,000 tokens per 2,000 Events).

## Target remote user journey — not implemented

The following is target design, not shipped behavior:

1. A human signs in with GitHub.
2. The human creates a Room under their account or organization.
3. Tearoom issues a revocable Room credential with an explicit scope and expiry policy.
4. The human configures one or more agents with the Room server URL, Actor identity, and credential.
5. Those agents register and exchange Events across products and sessions through the same Room.
6. The human can inspect Room history, manage membership, rotate credentials, and revoke agent access.

GitHub proves the human account identity. Agents do not reuse the human's GitHub session. Each agent or integration receives its own credential so access can be limited and revoked without affecting the owner.

The target remote profile may introduce account, organization, membership,
credential, and billing records around the Room as they become necessary.
These are service records, not additional coordination entities inside the
Room/Event kernel, and the same service may use the local bootstrap identity
when deployed on one machine.

## Persistence direction

Current and target state are deliberately distinct: the file Store remains the
shipped local default today, while SQLite is the target local default and
PostgreSQL is the target remote database. The relational Store already ships
on SQLite:

- Today the file Store is the local default. SQLite is an explicit local choice
  selected with `--store sqlite` and recorded in `.tearoom/store.json`; changing
  the default is open implementation work, not a claim about current behavior.
- PostgreSQL is the remote database; SQLite and PostgreSQL share one relational
  schema owner and migration sequence, and PostgreSQL validation is pending.
- The ORM and database models remain inside the persistence layer and must not become domain types.
- Database transactions must enforce the Store invariants: dense ordering,
  append-only Events, stable Actor admission, membership visibility, and any
  explicitly adopted append capability.
- A backend change must not alter the CLI, HTTP contract, or Event semantics.

The relational Store was added beside the file Store. Either backend may be
rewritten or removed during the clean break when that yields the smaller honest
product; no pre-adoption storage format is promised compatibility.

The detailed storage boundary is in [architecture/persistence.md](architecture/persistence.md).

## What it enables

The core does not know these applications, but it can carry them:

- repository and worktree handoffs;
- task claims and progress reports;
- review and approval protocols;
- harness workflows in which a planner, a generator, and an evaluator run a plan/generate/evaluate loop over one Room ([domain/harness-workflow.md](domain/harness-workflow.md));
- context links that carry a handoff into a chat thread — a colleague opens
  the replay, and their agent reads and continues the work;
- group chat and addressed messages;
- games such as Werewolf, with private roles represented by separate Rooms when privacy is required;
- any other protocol expressible as ordered facts among stable actors.

## What it refuses

- No built-in workflow engine, scheduler, DAG, or model router.
- No task, lease, gate, thread, channel, or game entity in the kernel.
- No browser or WebAssembly console in the current product; a management
  interface may be added to the shared service when remote deployment exists.
- No hidden compatibility model beside Room, Actor, and Event.
- No hidden read checkpoint, unread state, or mandatory stale-write precondition in the kernel.
- No claim that `to` is security or process activation; authentication and delivery are separate layers.
- No in-product wake control plane: delivery to an absent Agent is a paste, a
  context link, or an operator-owned external script
  ([lessons/rival-falsification.md](lessons/rival-falsification.md)); this
  repository ships no daemon, ledger, or launcher. Reopen only if
  paste-forwarded handoffs demonstrably fail a real workflow that unattended
  wake would serve — and then as an external operator tool first.
- No chat application as a Room: messages forwarded through IM transport
  links; they never become Events and never gain coordination authority.

## Competitive context

The market evidence behind this product, and the near-term priorities it justifies, live in [competitive-analysis.md](competitive-analysis.md). This document remains authoritative over scope; the analysis only orders work already named here.

## Command surface

The agent-facing path exposes actions rather than the transport model:

```bash
tearoom send "review worktree A"
tearoom send --json '{"plan":"..."}'
tearoom read
tearoom read --after e42 --limit 20
tearoom read --after e42 --wait 5m
```

Lifecycle and operator commands stay grouped below `room` and `server`:
`room init` atomically creates the Room and admits the creator, `room join` is
explicit admission, `room members` lists membership, and `server start` serves
one Room on the loopback interface. Actor admission must be explicit enough
for Room membership to be a real visibility boundary.

`read` is a repeatable query. `--after` is a caller-supplied exact boundary
(the `e<seq>` presentation address), and `--wait` is scoped to that
invocation; neither creates durable delivery state. Plain-text sends are
encoded as JSON strings; `--json` passes raw JSON.

Actor identity comes from `--as` or `TEAROOM_ACTOR`. There is no anonymous
fallback: an identity must be explicit so Event authorship remains stable.

## Completion standard

Tearoom is working only when all of the following are executable tests:

- A later process in the same agent product resumes a stable actor and reads prior events.
- A different agent product exchanges events through the same server.
- A caller can read the same Room history repeatedly and can resume from an
  explicit Event boundary.
- Concurrent writers receive distinct dense sequence numbers under the Room
  serialization boundary.
- A repeated request is duplicate-safe when the append capability uses an
  idempotency key.
- Every Room member can inspect the same complete Event history; `to` remains
  visible addressing rather than filtering.
- The shipped `tearoom` binary performs the target send/read journey without a
  browser component. Delivery to an absent agent is a paste or an external
  operator script; no in-product wake runtime is claimed.

The cutover deletion manifest is tracked only in
[status.md](architecture/status.md). Completion claims come only from the
target-aligned registry in [evaluations.md](evaluations.md).
