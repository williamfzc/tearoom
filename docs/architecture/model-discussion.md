---
type: architecture
title: Model rationale — the minimum Room and Event
description: Rationale and remaining hypotheses behind the adopted Room, Event, identity, and optional Agent activation model.
resource: internal/core/core.go
tags: [architecture, rationale, decision]
timestamp: 2026-08-30
---

# Model rationale: the minimum Room and Event

> **Status: adopted product rationale.** The 2026-08-29 and 2026-08-30
> discussion converged on the minimum model adopted by
> [product.md](../product.md). Open hypotheses remain open. The implementation
> still exposes the previous contract; that migration gap is recorded below and
> in [status.md](status.md).

## Thesis

Tearoom is, at minimum, a durable shared Room with membership and one ordered,
immutable history. A Room gives Events their container and order; an Actor is a
stable participant who can append and read; an Event records one contribution.

Shared history is independently useful: a deployment may use Tearoom only as a
simple shared space for people, Agents, or ordinary tools. An optional product
hypothesis is stronger: an activation service can observe an addressed Event
and activate a temporarily absent Agent, which re-enters the Room under its
stable Actor identity, reads the relevant history, acts, and contributes the
result. That recoverable collaboration loop may differentiate Tearoom from
messaging, but it is not required for a Room to be valid or useful.

Anything beyond that minimum needs usage evidence. In particular, Tearoom must
not assume that workflow types, causal links, unread state, or mandatory
optimistic concurrency belong in the kernel merely because the current
implementation contains them.

## 1. Entity and value

Rooms and Actors are entities: they have identity, lifecycle, membership, and
authorization. Events are immutable values in a Room's sequence. An Event may
be addressed by its sequence, but it has no independent lifecycle or
credentials.

The smallest Event shape discussed so far is:

```text
Event ::= {
  seq,
  actor,
  at,
  to?,
  content
}
```

- `seq` is the Event's position in its Room and its precise replay address.
- `actor` is the author, established by Room identity rather than trusted from
  caller-provided content.
- `at` is assigned when the Event is accepted.
- `to` is optional addressing or attention metadata naming Room members.
- `content` is the contribution. Its exact text/JSON shape is still open.

`room` need not be repeated inside the Event when the container already owns
that fact. An `e<seq>` identifier may be a presentation of `seq` rather than a
second stored identity. `type` and `caused_by` are not part of this minimum
schema. A protocol may prove that it needs an envelope containing such fields,
but the kernel must not pre-empt that evidence.

## 2. Visibility is membership

Everyone in a Room can see the Room's complete Event history. Visibility is a
property of the container, not of individual content.

`to` is still a first-class Event field, just as `at` is, but it answers a
different question:

```text
Who may read this Event?   Every member of the Room.
Whom does it address?      The members named by Event.to, if any.
```

An absent `to` means that the Event has no particular addressee. It does not
encode broadcast permissions and must not implicitly wake every member. A
populated `to` is visible to every Room member and acts like a mention or
routing hint, not a secrecy or process-control mechanism. An optional activation
layer may observe it when selecting Agent runtimes, subject to an independently
configured policy; neither the Event nor `to` itself requests or starts a
process.

Privacy therefore requires a separate Room with separate membership. This is
only a coherent security boundary after admission is real: reading or sending
must not silently create membership. The current local auto-registration path
does not yet satisfy that requirement.

## 3. No implicit read checkpoint

Tearoom does not know whether an Actor understood, handled, or committed an
Event merely because bytes reached a CLI process. It therefore should not own
per-Actor unread state, acknowledgements, or an implicit saved read cursor.

Reading is a repeatable, side-effect-free query:

```text
tearoom read
tearoom read --after e42
tearoom read --since 30m
tearoom read --after e42 --wait 5m
```

- With no boundary, `read` returns the current visible history.
- `--after e42` is the precise incremental boundary supplied by the caller.
- Time is a human-friendly filter, not a correctness boundary: timestamps may
  coincide, while `seq` defines the Room's order.
- `--wait` waits within that invocation; it does not persist a subscription or
  progress after the command exits.

The Room still has a head and Events still have sequence addresses. Those are
log structure, not a record of where any Actor has read. A future real consumer
that needs durable delivery progress must introduce an explicit consumer or
subscription identity instead of borrowing Actor identity invisibly.

## 4. Append and freshness

The Store assigns every accepted Event the next dense Room sequence under its
serialization boundary. An ordinary append must not depend on hidden read
progress: several Room members may contribute concurrently and the Store orders
their Events.

Some decisions may eventually require a conditional append such as "accept
this only if the Room head is still e42." That remains an explicit Store
capability and an open product question, not the default meaning of `send`.
Room-wide head comparison is also coarse: an unrelated contribution would
invalidate a task claim. A real protocol must prove whether this protection is
useful before the CLI gains an `--if-head`-style surface.

Idempotent append remains independently useful for uncertain retries; it does
not require a saved read checkpoint.

## 5. Agent-facing surface

The common path should expose actions rather than the transport model:

```text
tearoom send <content>
tearoom read [--after <event>] [--since <time>] [--wait <duration>]
```

Low-frequency lifecycle and operator commands may remain grouped below `room`,
`server`, or `admin`. The current `event list` and `event watch` names are not
honest: `list` mutates saved state and `watch` performs one blocking read rather
than continuous observation. Waiting is a mode of pure `read`, not a separate
durable subscription.

The normal output should be line-oriented Events without a feed envelope or
cursor header. Exact JSONL/text details remain a CLI contract question.

## 6. Recursion is a deferred hypothesis

A nested Room could provide a smaller membership boundary:

```text
Room ::= Event*
Room may contain child Rooms
```

If recursion lands, hierarchy is structure, not merged order. A parent never
folds child sequences into its own dense sequence. Child creation still needs
one source of truth and an honest failure contract across the file and
relational backends.

No sub-Room, birth record, `spec.json`, strict Room type, template plugin, or
referee is justified by the minimum model yet. These remain candidate protocol
mechanisms, not current kernel design.

## 7. Is this only IM?

With membership, ordered history, authors, timestamps, addressees, append,
read, and wait, Tearoom's user-visible surface is a small group-messaging
system. Append-only storage, JSONL, and a Unix CLI are implementation choices;
they do not by themselves make a new product category.

The remaining hypothesis is narrower: a neutral, durable, machine-facing Room
may let agents from different products resume work without a human relaying
context. Dense order and idempotent append may make that shared history safer
to automate than an ordinary human IM channel. This is not yet proven by the
scripted demos.

If an existing IM channel with bot credentials satisfies the same real
multi-session journey at comparable integration and attention cost, Tearoom has
no demonstrated differentiation for that journey. A user may still prefer the
small local shared-space surface, but the next product evidence for a broader
claim must compare that journey rather than add more model machinery.

## 8. Actor identity and Agent activation

Activation is an optional application capability above the minimum Room. A
storage-only deployment needs only durable `send` and `read`. An online client
may additionally use invocation-scoped `read --wait`. A deployment that wants
temporarily absent Agents to act may add activation:

```text
shared space       Room + membership + send + read
online waiting     shared space + read --wait
Agent activation   shared space + runtime bindings + activation service
```

These modes share the same Room and Event model. Enabling activation must not
change Event visibility, append semantics, or what `to` means.

An Actor and a running Agent are not the same thing:

```text
Actor              durable identity and Room membership
Credential         proof that a caller may act as that Actor in that Room
Runtime binding    operator-owned description of how that Actor can run
Runtime            one ephemeral execution of the Agent
Activation         delivery of an Event reference to a Runtime or launcher
```

Only Actor belongs to the minimum collaboration model. Credentials belong to
authorization. Runtime bindings, Runtimes, and activation delivery belong to an
application or control-plane layer above the Room kernel. A credential proves
who a caller is and what it may do; it is neither a process identity nor a wake
destination.

When activation is enabled for Bob, the optional collaboration loop is:

```text
an Event addressed to Bob is appended durably
  -> Bob's configured activation policy elects or ignores it
  -> Bob's runtime binding selects a launcher
  -> the launcher starts or resumes Bob
  -> Bob receives Room, Actor, and triggering Event reference
  -> Bob reads the Room, acts, and appends a contribution
```

The trigger carries a reference, not a copied context transcript. The Runtime
can read the exact triggering Event and whatever Room history its task needs.
This preserves the Room as the source of truth and does not require an implicit
Actor read cursor.

### Delivery state is not read state

Reliable offline activation cannot be derived from an in-memory notification.
An activation worker may need durable operational state keyed by:

```text
(room, event sequence, target actor)
```

That state records whether a wake attempt is pending, claimed, delivered, or
failed. It must not claim that the Actor read, understood, or completed the
Event. Delivery should assume at-least-once attempts, and the launcher and
Runtime must make duplicate triggers safe. This ledger is an explicit consumer
mechanism owned by the activation service, not hidden progress attached to the
Actor and not part of the Room's collaboration history.

The activation policy must also bound cost and authority: which addressed
Events are eligible, which senders may cause a run, which launcher may run,
concurrency per Actor, and applicable rate or budget limits. Addressing Bob does
not by itself request execution or grant permission to spend Bob's resources.

An online Runtime can wait for Events. Waking an offline Agent requires a
launcher outside the Room kernel, such as a local daemon, webhook receiver, or
queue consumer. Tearoom should first prove one local launcher rather than
prematurely becoming a general process scheduler.

### Reference designs

Several mature systems solve parts of this path, but none should define the
Room model:

- Temporal persists Workflow history and places work on Task Queues polled by
  already-running Workers. Its useful lesson is durable intent plus replay, not
  that a Room should become a deterministic Workflow.
- NATS JetStream durable consumers track acknowledgement, redelivery, delivery
  limits, and backpressure. Its useful lesson is at-least-once delivery with
  explicit consumer state, not that broker acknowledgement means an Agent
  completed a collaboration task.
- Restate Awakeables persist a callback token and resume suspended work after
  an external response. They are a useful completion primitive, but an
  Awakeable is not a stable Actor or shared Room history.
- `launchd`, systemd, and D-Bus activation demonstrate the local process
  boundary: a small supervisor or endpoint can remain available while an
  expensive service process is started only on demand. They do not provide
  Room delivery semantics themselves.

The August 2026 `grok-bot-0.18-reconstructed` repository is an unofficial
source-oriented reconstruction from a distributed Grok Bot application, not a
confirmed release of Anysphere's original source. It is evidence about shipped
behavior, with that limitation. Its useful patterns include:

- external trigger adapters are separated from execution;
- a fire is revalidated against enabled state, trigger matching, and definition
  revision before it runs;
- each Agent has one active run, with queued user, Agent, and background lanes;
- wake identifiers and grouping keys suppress redundant launches;
- pending background wake markers survive host restart and are rearmed.

Tearoom can borrow those behavior patterns without copying reconstructed code.
In particular, quiet hours, authorization, budgets, and executable bindings
must be enforced as code-owned policy. Prompt instructions may express a soft
preference, but they are not an execution safety boundary.

### Relationship to Grabhub

The sibling Grabhub product already implements a narrower form of durable
activation:

```text
Schedule -> Execution -> Trace
```

The canonical Grabhub sources for this comparison are
`grabhub/docs/product-design/scheduled-trigger-model.md`,
`grabhub/internal/grabhub/service.go`, and
`grabhub/internal/grabhub/schedule_runtime.go` in the sibling repository.

Its service polls on a short interval, selects schedules whose
`next_fire_at <= now`, atomically claims one occurrence with
`claimed_execution_id`, launches the configured ACP provider with a preset
prompt, records process-level evidence, and advances or ends the Schedule. A
due Schedule does not occupy a momentary notification window: it remains due
until claimed. A late or restarted service can therefore discover it again.

That is structurally similar to Event activation:

```text
Grabhub:  due time       -> claim Schedule occurrence -> run preset prompt
Tearoom:  addressed Event -> claim Actor activation    -> read Room and act
```

The overlap is the small runtime boundary: discover durable intent, claim it
atomically, start an Agent process, record execution-level evidence, and recover
after restart. Grabhub also keeps execution status honest: process success does
not mean that the scheduled goal succeeded. Tearoom should retain that
distinction between a settled Runtime invocation and a collaboration outcome.

The products are not the same model. Grabhub has no Room, shared history, stable
participant addressing, or event-driven handoff; its current product boundary
explicitly excludes signal ingestion and alternate queue or worker models. A
Schedule owns a prewritten prompt and trigger time. A Tearoom activation owns a
reference to an immutable Event; the Agent reconstructs context from the Room.
Grabhub's service heartbeat prevents duplicate service instances, while its
schedule claim parks one occurrence until normal completion or restart
recovery. That is not yet a renewable per-activation lease or a guarantee
against repeated external effects. Even an old claim remains parked during
ordinary ticking; only service recovery clears it. The current behavior is
restart recovery, not a general lease-expiry retry policy.

The honest reuse direction is therefore design reuse first, not repository
coupling. Grabhub's due-query, atomic-claim, execution-evidence, service-guard,
and restart-recovery patterns can inform a Tearoom activation boundary. Its
Schedule schema and schedule-only service loop must not be imported into the
Room kernel or expanded in place to become Tearoom's event runner.

### Minimum reliable activation path

The first implementation should keep four explicit owners:

```text
Room Event log       durable collaboration truth
Activation ledger   claim, lease, attempt, retry, budget, and failure state
Runner host          operator-owned Actor bindings and process supervision
Agent Runtime        Room reading, domain action, and Event append
```

For each Event elected by an enabled policy, an activation ID can be derived
from `(room, seq, actor)` so a repeated scan creates the same work identity. A
relational activation service may consume an outbox or project from the Event
log. A file-backed service can rescan ordered Events and insert the deterministic
activation idempotently. Its scan checkpoint is rebuildable operational state,
not Actor read progress. When no activation service is configured, append stops
after recording the Event; it must not create dormant delivery machinery.

A Runner claims pending or expired work atomically, records a bounded lease,
and initially permits only one active invocation per Actor. It starts the
configured command with an activation reference, never with Event content
interpolated into a shell command. A long-running invocation renews its lease;
lease expiry permits another attempt. Online notification may lower latency,
but periodic reconciliation against the durable ledger remains the recovery
path.

The system should promise at-least-once activation, not exactly-once execution.
Result Event append can be idempotent by activation ID. External effects such as
deployment, payment, or deletion require their own idempotency or approval
contract. Runtime settlement records that one invocation ended; only Events in
the Room express what the collaboration achieved.

## 9. Activation product proof

The first credible vertical slice for the optional activation capability is
deliberately narrow:

1. Bob is a Room member but has no running process.
2. A permitted Actor appends an Event whose `to` names Bob.
3. A local daemon discovers the durable Event and starts Bob's configured
   command; duplicate discovery is safely coalesced or retried without an
   unsafe duplicate effect.
4. Bob receives its Actor identity and the triggering Event reference, reads
   the Room, and appends a response.
5. Daemon restart preserves the activation opportunity, while an Event that
   Bob's policy does not elect causes no process execution.

A trivial responder should prove this mechanism before a model runtime is
integrated. A `plan -> develop -> verify` harness can then prove multi-Actor
handoff and human intervention. A substantially different second use case,
such as a game referee waking the next player, is evidence for an extension
boundary. It is not a reason to design a plugin SDK in advance.

The shared-space product does not depend on this experiment. Until the
offline-Bob journey works, however, Tearoom may claim a collaboration record
layer but not a demonstrated Agent activation runtime. `read --wait` waking an
already connected process is insufficient proof of that optional capability.

## 10. Adopted and open

**Adopted in the product definition:**

1. Room membership, not Event content, determines visibility.
2. Every Room member sees the complete history; `to` is public addressing or
   attention metadata in the Event schema.
3. Tearoom owns no implicit read checkpoint, unread state, or acknowledgement;
   reading is a pure query whose boundary belongs to the caller.
4. The minimum Event discussion contains `seq`, `actor`, `at`, optional `to`,
   and `content`; `type` and `caused_by` are not assumed kernel fields.
5. Ordinary append does not derive a mandatory write precondition from read
   progress.
6. The normal CLI path is `send` and `read`; waiting is a `read` mode.
7. Actor identity is durable and independent of any Agent process or session.
8. `to` remains public addressing. An optional activation service may observe
   it, but process execution is neither encoded nor requested by the Event.
9. Activation delivery state, if required, is explicit operational consumer
   state and must not be presented as Actor read or task-completion state.
10. A Room remains valid without runtime bindings, an activation service, or
    any Agent process.

**Explicitly open:**

1. Whether reliable offline activation and recoverable collaboration prove
   value beyond an existing IM channel, queue, or workflow engine.
2. The exact `content` schema and JSONL/text CLI contract.
3. Whether any real protocol needs conditional append, idempotency keys exposed
   to callers, typed envelopes, or causal references.
4. Admission and membership management, especially before sub-Room privacy can
   be claimed.
5. Whether recursive Rooms are necessary, and the atomic child-creation
   contract if they are.
6. Event retention and compaction.
7. Whether direct `cat`/`tail -f` access to the file log is supported or remains
   an implementation detail.
8. The first runtime-binding format, launcher contract, activation policy, and
   safe duplicate-handling contract.
9. Whether durable activation delivery is necessary after the local vertical
   slice, and which application-layer component owns it.

## 11. Migration gap in the current implementation

The product definition adopts this model before the executable does. The sole
implementation-gap inventory and clean-break sequence live in
[status.md](status.md); the proof state lives in
[evaluations.md](../evaluations.md). This rationale does not preserve or specify
the rejected implementation as an alternate contract.
