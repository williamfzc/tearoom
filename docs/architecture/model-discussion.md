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
> [product.md](../product.md). Open hypotheses remain open. The pre-cutover
> implementation exposed the previous contract until the P0 clean break removed
> it; that migration is recorded below and in [status.md](status.md).

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
- `content` is the contribution and may be any valid JSON. The Store validates
  and compacts it for persistence; application-level protocol schemas remain
  open.

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
`server`, or `admin`. The pre-cutover `event list` and `event watch` names were
not honest: `list` mutated saved state and `watch` performed one blocking read
rather than continuous observation. Waiting is a mode of pure `read`, not a
separate durable subscription.

The normal output is line-oriented Events without a feed envelope or cursor
header. The shipped JSONL/text behavior is defined by the [CLI contract](../reference/cli.md);
future application-level content conventions remain open.

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

Reliable offline activation needs explicit delivery-control state, but that
state is neither Room history nor Actor read progress. It must distinguish a
claimed or settled invocation from an Agent contribution and bound who may
spend an Actor's runtime resources. The adopted component boundary, identity
model, state machine, lease fencing, retry, crash recovery, launcher contract,
security controls, reference systems, and first vertical slice live in the
canonical [activation technical design](activation.md).

The rationale remains: an online Runtime can wait, but waking an offline Agent
requires an operator-owned launcher outside the Room kernel. The design must
first prove one local launcher and a trivial responder instead of turning
Tearoom into a general scheduler or Runtime platform.

## 9. Activation product proof

The offline-Bob journey remains the falsifiable product hypothesis. Its detailed
technical contract is [activation.md](activation.md), while P2/E5 in
[evaluations.md](../evaluations.md) is the sole proof definition. Until that
evaluation passes, Tearoom may claim a durable collaboration record but not a
demonstrated offline Agent activation runtime. An already running process using
`read --wait` is not that proof.

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
2. The exact application-level `content` schema and future protocol
   conventions; the shipped JSONL/text CLI contract is fixed in
   [reference/cli.md](../reference/cli.md).
3. Whether any P0/P1 protocol needs conditional append or typed/causal envelope
   fields. Request-level append idempotency is required only if P2 activation
   proceeds and remains outside the Event schema.
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

## 11. Migration gap and its resolution

The product definition adopted this model before the executable did; the P0
clean break then replaced the executable without aliases or compatibility
paths. The sole implementation-gap inventory and clean-break sequence live in
[status.md](status.md); the proof state lives in
[evaluations.md](../evaluations.md). This rationale does not preserve or specify
the rejected implementation as an alternate contract.
