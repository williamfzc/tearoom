---
type: architecture
title: Optional Agent activation
description: Technical design for turning an eligible durable Event reference into a bounded, restart-safe Agent invocation without changing the Room kernel.
tags: [architecture, activation, runtime, reliability, security]
timestamp: 2026-08-30
---

# Optional Agent activation

> **Status: design-ready, not implemented.** This document is the canonical
> technical design for P2 activation. [product.md](../product.md) owns the
> product boundary, [status.md](status.md) owns implementation state, and
> [evaluations.md](../evaluations.md) owns executable proof.

## Decision

Activation is an optional application control plane above the Room. It turns an
eligible durable Event reference into a bounded attempt to run one admitted
Actor. It is not part of `core.Store`, does not change `to`, and is absent when
an operator only wants a shared Room.

The first implementation is one local daemon, one fixed Actor binding, one
Room, one policy, and one trivial responder. It uses the Room Event log as the
durable source and a separate activation ledger for delivery control. It does
not introduce a broker, workflow engine, scheduler, plugin SDK, or hosted wake
service.

```text
Room Event log
    -> durable scan and policy election
Activation ledger
    -> atomic claim, lease, and bounded retry
Operator-owned launcher
    -> activation and Event references
Agent Runtime
    -> explicit Room read and idempotent result append
```

## User journey and invariants

1. Alice and Bob are admitted Room members. Bob has a stable Actor identity and
   an enabled runtime binding, but no running process.
2. Alice appends Event `e1` with `to` naming Bob. The append commits independently
   of activation availability.
3. The daemon discovers `e1`, authorizes it under Bob's operator-owned policy,
   and idempotently creates one activation.
4. A runner claims that activation and launches Bob with Room, Actor,
   activation, and Event references. It never interpolates Event content into a
   command.
5. Bob explicitly reads `e1` and any history it needs, acts, and appends one
   duplicate-safe response Event `e2`.
6. After daemon failure or restart, unfinished work remains discoverable and a
   retry cannot create a second collaboration response.

The design preserves these invariants:

- The Room Event log is the collaboration truth. The activation ledger is
  separate durable delivery-control state. Logical activation identities can
  be reconstructed from Events, but attempt and budget history must be retained
  for safe operation.
- `to` is public attention metadata, not execution permission.
- An activation state never means that an Actor read, understood, or completed
  an Event.
- Runtime exit never means that the collaboration or an external side effect
  succeeded. Only Room Events express collaboration results.
- Delivery uses bounded at-least-once attempt semantics: eligible work is
  retried after uncertain failure while its limits permit, then reaches an
  explicit terminal state. Exactly-once process execution and exactly-once
  external effects are not promised.
- With no activation service, Event append is still complete and creates no
  hidden work or Actor cursor.

## Ownership and dependency direction

| Owner | Layer | Responsibility |
|---|---|---|
| Room Event log | domain and persistence | Membership, immutable Events, dense order, explicit reads, and append |
| Activation projector | application | Scan Events, evaluate policy, and idempotently create eligible work |
| Activation ledger | application persistence | Claim, lease, attempt, retry, budget, cancellation, and terminal failure state |
| Runner and launcher | outside | Resolve an operator binding, start a constrained process, and observe invocation settlement |
| Agent Runtime | outside client | Authenticate as the Actor, read the Room, perform domain work, and append results |

`core` must not import activation packages or know about a process, lease,
binding, scheduler, or consumer checkpoint. The activation service consumes the
same public Store contract as another application and may append only through
that contract.

## Identity model

```text
Actor identity       stable Room participant
Credential           proof that a caller may act as that Actor
Runtime binding      operator-owned launch configuration for that Actor
Activation ID        one logical Event-to-Actor delivery
Attempt number       one claim and invocation attempt for that activation
Lease generation     fencing value for the current claimant
Runtime session      optional provider- or process-specific execution identity
```

The logical identity is deterministic:

```text
activation_id = stable(room_id, event_seq, target_actor)
```

The ledger enforces uniqueness on `(room_id, event_seq, target_actor)`. Policy
and binding revisions are audit snapshots but are not part of the identity:
changing configuration must not silently duplicate the original work. An
operator retry reuses the activation and creates a new attempt. Backfilling an
Event that was previously ineligible requires a separate explicit action.

An activation consumer checkpoint is allowed only as a named, rebuildable scan
optimization. It does not say where Bob read. A scanner may resume from an
earlier sequence and rely on the uniqueness constraint to coalesce discovery.

## Eligibility and policy

An Event becomes candidate work only when it explicitly addresses the bound
Actor. Missing `to` never wakes every member. Addressing is necessary but not
sufficient; election also requires:

```text
target Actor is still admitted
+ target binding is enabled
+ sender is permitted
+ Event is at or after the binding's activation boundary
+ Room and Actor policy permit another run
+ quiet hours, rate, concurrency, and budget checks pass
```

The first slice supports exact sender allowlists rather than a policy language.
It does not inspect content to infer a workflow type, urgency, or authority.
Actor self-addressed Events are ignored by default to prevent an unbounded
feedback loop. A policy denial creates no activation; a later policy change
does not replay historical Events unless the operator explicitly requests it.

`enabled_after_seq` is exclusive: only Events with
`event_seq > enabled_after_seq` are eligible. Initial enable and ordinary
re-enable set it to the current Room head, so activation never wakes from old
history by surprise. An explicit backfill action may choose an earlier boundary
and is still coalesced by the activation uniqueness key.

The binding is host configuration, not Room content. Its minimum contract is:

```text
room and actor
enabled and enabled_after_seq
absolute executable and fixed argv
working directory
credential reference
allowed senders
maximum runtime and attempts
quiet hours and rate/budget ceilings
operator owner and revision
```

The activation projector reads only configured Rooms through a read-only
credential scoped to an explicitly admitted service Actor. It has no
cross-Room discovery authority and no append capability. The binding's
credential reference names a host secret; it is not the secret itself. Secrets
are never stored in an Event, binding document, activation ledger,
command-line argument, or ordinary log.

## Durable discovery

The first slice does not need an outbox. The ordered Event log is already the
durable intent source:

1. The projector scans from its explicit service checkpoint.
2. It evaluates only admitted targets with enabled bindings.
3. In one ledger transaction, it inserts eligible activations using the
   deterministic uniqueness key and advances the checkpoint through the last
   inspected immutable Event.
4. Startup and periodic reconciliation repeat from the durable checkpoint;
   explicit repair may rescan any earlier range safely.

An append-time notification may reduce latency, but it is only a hint. Correct
recovery always comes from rescanning the Event log. A transactional outbox is
justified only if Event and activation stores later move to different systems
and measured discovery latency cannot be met by scanning; it remains
application-owned.

## Ledger and state machine

```text
activation_id
room_id, event_seq, target_actor
policy_revision, binding_revision
state                         pending | leased | retry_wait | settled | dead | cancelled
attempt_count, next_attempt_at
lease_owner?, lease_generation?, lease_until?
created_at, updated_at, settled_at?
last_error_class?, last_error_summary?
```

Each claim creates an append-only attempt record containing its attempt number,
lease generation, runner identity, phase (`claimed`, `started`, or `finished`),
claim/start/finish times, safe error classification, process exit evidence, and
optional result Event sequence. These attempt phases record whether launch was
observed without expanding the activation state machine.

```text
pending -> leased -> settled
             |
             +-> retry_wait -> pending
             +-> dead

pending | retry_wait | leased -> cancelled
```

For the first responder, `settled` requires observing the idempotent response
Event as well as the Runtime ending. A zero exit without that response moves to
`retry_wait` or, after exhaustion, `dead`. Settlement still does not mean Bob's
domain task succeeded; the response may report failure. A later Runtime
contract that legitimately produces no response must define another durable
receipt before it can change this gate. The ledger itself is the first
dead-work view; a separate queue or dead-letter entity is unnecessary.

## Claim, lease, and single flight

A runner claims with one transaction from due `pending`, due `retry_wait`, or
an expired `leased` record. That transaction must also acquire an Actor lease
keyed by `(room_id, target_actor)`; a uniqueness constraint permits only one
current Actor lease even when different activations are pending. The claim
increments `lease_generation`, assigns an opaque owner, and sets a bounded
`lease_until`. Renew, settle, retry, cancel, and fail operations must match both
activation ID and lease generation, and release the matching Actor lease. A
superseded runner cannot overwrite a newer attempt's control state.

Lease fencing protects the ledger, not arbitrary external effects. An expired
process may still be alive. The first slice issues only one current lease per
Actor, uses a hard runtime timeout, and requires duplicate-safe result append.
This serializes normal launches but cannot prove that a superseded physical
process stopped. A reclaimed attempt is still at-least-once execution and must
be treated as such.

Multiple pending Events for one Actor remain explicit work. The first slice
processes them in Room sequence after the active invocation settles; it neither
silently merges nor drops them.

## Launch contract

The runner invokes an absolute executable directly with structured argv. It
must never call `sh -c`, interpolate Event content, or accept a command supplied
by a Room member. The Runtime receives only references equivalent to:

```text
activation_id
room_id
actor_id
event_seq
attempt_number
lease_generation
```

The launcher uses a fixed working directory, a cleaned environment, bounded
time and process resources, and a host-owned credential channel. The Unix local
slice resolves the credential reference from a mode-0600 file or OS secret
store, writes the secret once to an anonymous pipe inherited as file descriptor
3, exposes only the descriptor number, then closes both parent pipe ends. The
child reads once and closes the descriptor. Plaintext environment variables and
argv are forbidden. Logs include reference IDs and error classes, not Event
bodies or secrets.

The Runtime then authenticates through the normal Room interface, reads the
trigger and any required history, and appends its contribution. Prompt
instructions may guide the Agent, but policy, authorization, budgets, and
executable selection are code-owned boundaries.

## Completion and idempotency

P2 has a hard dependency on request-level append idempotency outside the Event
schema. A response append uses a key derived from the activation and response
role, such as `activation/<activation_id>/response`. The Store keeps a scoped
mapping from `(room, actor, request key)` to the accepted Event and exposes an
authenticated way for that Actor to resolve the key. The key is request
metadata, not an Event field.

On every attempt, the Runtime first resolves its response key. If an Event
already exists, it reports that sequence to the runner and does no model or
external work. Otherwise it produces the response, appends with that key, and
reports the returned Event sequence over a structured runner channel. The
runner validates that the Event exists and was authored by the target Actor
before writing `result_event_seq` into the attempt receipt and settling. If the
Runtime crashes after append but before reporting, the next attempt resolves
the same key and recovers the receipt without a second Event.

This protocol prevents duplicate collaboration Events after an uncertain
runner crash.
It does not make deployment, payment, deletion, email, or another external
effect exactly once. Such effects require their own provider idempotency key,
durable receipt, or human approval. The first responder performs no external
side effect.

## Retry and recovery

The daemon owns a narrow due-work and reconciliation loop for activation only;
the refusal is a general-purpose cron or workflow scheduler. Transient launch,
network, timeout, and unknown process failures use bounded
exponential backoff with jitter. Invalid binding, revoked credential, missing
membership, policy denial after claim, and exhausted budget are terminal or
cancelled conditions rather than automatic retry loops. Every binding has both
a maximum attempt count and maximum elapsed duration. The guarantee is not
eventual success: while an activation remains eligible and within those limits,
the daemon makes or retries an attempt; otherwise it records `dead` or
`cancelled` for operator inspection.

| Failure point | Durable fact | Recovery |
|---|---|---|
| Before Event append | No Event | Create no activation |
| After append, before discovery | Event exists | Reconciliation creates the deterministic activation |
| After activation insert, before claim | `pending` exists | A runner claims it |
| After claim, before launch | Lease and attempt exist | Lease expires, then a new generation may retry |
| After launch, before start evidence | Runtime may exist | Treat outcome as unknown; fence ledger updates and retry at least once |
| After response append, before receipt | Response Event and request-key mapping exist | The next Runtime resolves the key, reports the original sequence, and avoids repeated work |
| After process exit, before settle | Outcome may be unknown | Lease recovery may retry; response and external effects remain independently idempotent |
| Daemon restart | Events and ledger remain | Rescan Events, recover timers, and reclaim only expired work |

Startup reconciliation also checks missing Events, stale leases, missing runner
owners, and budget exhaustion. It never copies a Room transcript into the
ledger.

## Security and cost boundaries

The E5 slice is no-go unless it enforces these baseline controls in code:

- sender allowlist, target opt-in, current Room membership, and scoped
  credential validation;
- fixed executable and argv, no shell interpolation, and no Event body in the
  launcher contract;
- one current lease per Actor, a hard timeout, bounded retry, rate limits,
  and a fixed per-Actor activation ceiling;
- no plaintext secrets in Events, bindings, ledger rows, argv, or ordinary
  logs;
- bounded Event size at the Room contract and safe parsing by the Runtime;
- a terminal `dead` state plus operator-visible inspection and a binding-disable
  action;
- an explicit external-effect idempotency or approval contract whenever a
  Runtime can perform an irreversible action.

Before a model Runtime or shared deployment, add quiet hours, Room and Actor
budgets, explicit manual retry/cancel, and auditable policy overrides. A first
local trivial responder does not need a billing-grade quota system or control
console.

An addressed Event cannot spend another Actor's resources by itself. Webhooks,
remote launchers, and multi-tenant hosted execution add separate authentication,
network, sandbox, and abuse problems and are deferred.

## First vertical slice

1. Complete P0/P1, including request-level append idempotency.
2. Add an application-owned local ledger and deterministic projector for one
   configured Room and Actor.
3. Add one daemon runner with atomic claim, generation fencing, a fixed command,
   an Actor-level lease, timeout, bounded retry, and startup reconciliation.
4. Add a trivial responder that reads the triggering Event by reference and
   idempotently appends one response.
5. Prove E5 by killing the daemon before claim, after claim, after launch, and
   after response append; also run two claimers against two activations for the
   same Actor and prove only one current Actor lease is issued.
6. Measure wake latency, idle cost, retries, and duplicate attempts before
   integrating a model Runtime.

The implementation may use a small SQLite ledger. It should rely on the host's
process supervisor for daemon lifecycle; Tearoom owns Event-to-Actor delivery
semantics, not OS service management.

## Deferred and refused

Deferred: distributed scheduling, multiple queue backends, priority and
fairness, arbitrary webhook plugins, policy DSLs, cross-Room fan-out,
multi-Runtime election, hosted execution, provider-session abstraction,
automatic context compression, and a graphical dead-work console.

Refused: execution solely because `to` names an Actor; Actor read cursors as
delivery checkpoints; acknowledgement or process exit presented as
collaboration completion; Event content or credentials in commands; infinite
retry or exactly-once execution claims; scheduler, queue, lease, or process
ownership in the Room kernel; protocol interpretation by the activation
service; and a general plugin SDK for the first launcher.

## Reference designs

The design borrows narrow mechanisms rather than product models:

- [Temporal Activity execution](https://docs.temporal.io/activity-execution),
  [Task Queues](https://docs.temporal.io/task-queue), and
  [Event History](https://docs.temporal.io/workflow-execution/event) motivate
  durable intent, worker pull, timeout, heartbeat, and recovery. A Room is not a
  deterministic Workflow.
- [NATS JetStream consumers](https://github.com/nats-io/nats.docs/blob/master/nats-concepts/jetstream/consumers.md)
  demonstrate durable consumer identity, explicit acknowledgement, redelivery,
  delivery limits, and backoff. An acknowledgement is not Actor comprehension.
- [Restate external events](https://docs.restate.dev/develop/ts/external-events)
  and [architecture](https://docs.restate.dev/references/architecture) motivate
  durable wait identities, idempotent invocation, and generation fencing. They
  do not make external Agent effects exactly once.
- [systemd socket activation](https://www.freedesktop.org/software/systemd/man/252/systemd-socket-activate.html)
  and Apple's [launchd configuration](https://github.com/apple-oss-distributions/launchd/blob/main/man/launchd.plist)
  support a small supervisor starting expensive work on demand, but do not
  provide durable Event delivery.
- [OpenClaw heartbeat](https://github.com/openclaw/openclaw/blob/main/docs/gateway/heartbeat.md)
  and [restart recovery](https://github.com/openclaw/openclaw/blob/main/docs/gateway/restart-recovery.md)
  distinguish periodic attention from recovery of durable operational intent.
- [A2A asynchronous operations](https://a2a-protocol.org/latest/topics/streaming-and-async/)
  treats a push notification as a reference after which the receiver fetches
  canonical state. Tearoom likewise passes an Event reference while the Room
  remains the collaboration record.

The sibling Grabhub implementation is a local design analogue: its due query,
atomic claim, execution evidence, service guard, and restart recovery inform
the daemon boundary. Its Schedule schema and schedule-only loop must not move
into Tearoom's Room kernel. Process settlement is not proof of domain success.
