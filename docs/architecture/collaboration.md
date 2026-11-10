---
type: architecture
title: Room collaboration delivery
description: Proposed local role configuration, hc panel routing, and recoverable handoff contract.
tags: [architecture, collaboration, hc, delivery]
timestamp: 2026-11-08
---

# Room collaboration delivery

This is the local integration for the product and
[user journeys](../domain/user-stories.md). One unattended builder–reviewer
workflow has been exercised; wider acceptance remains open.
The [implementation status](status.md) records what ships. The kernel owns
Room history; this integration owns configuration, delivery and recovery.
The [product boundary](../product.md#conversation-and-runtime-policy) leaves
Agent context management and work decisions to the Agent and its tool.

## Roles and Room panels

Define a reusable role with a stable name, description, agent command,
working-directory setting, and role instructions. Role instructions are an
initial message, not a guaranteed system prompt. Credentials
remain with the agent environment; Room messages and role descriptions do not
store secrets.

Room creation may select role definitions; they can also be changed later. The roster is discoverable
through the CLI and is routing configuration, not permission to read or write.
One Room-role binding identifies one logical panel and at most one active hc
terminal at a time. Different Rooms using the same definition have separate
terminals and conversations. A panel may be created on demand; replacing a
terminal does not itself replace or resume the Agent conversation. A CLI view should
show the role name, description, agent type and observed availability, including
unknown or blocked; it must not claim that an absent or unready panel is ready.
Configuration and commands are documented in the
[Agent integration guide](../reference/agent-integration.md).

### Conversation continuity and replacement

The [product policy](../product.md#conversation-and-runtime-policy) requires
one continuing Agent conversation for each Room-role instance. On its first
wake, start a conversation; later addressed messages go to that conversation,
queued while it is busy. A persistent panel alone is insufficient if the
program inside it starts a fresh conversation on each turn. Role definitions
remain reusable across Rooms, but their conversation histories are separate.
The current implementation starts a new conversation with each new panel;
Tearoom does not bind or resume an Agent session identity.

The product requires recovery of the existing conversation when supported,
and visible reconciliation when its state is uncertain. The current automatic
missing-panel path instead starts a new conversation; this is an implementation
gap, not proven continuity. An intentional fresh conversation should be
an explicit replacement of the Room-role instance, with the prior outcome and
any pending delivery reconciled first. Room records remain the durable shared
history for rebuilding context; conversation and panel identifiers are runtime
integration state, not new product entities or Record fields.

`tearoom collab restart --room <room> --role <role>` is that explicit
replacement: it stops the live panel and starts the configured command again.
It refuses while the panel is working and refuses, without `--confirm`, while
a submitted or uncertain delivery still awaits reconciliation. A replacement
is a new conversation: Tearoom stores no conversation identifier and does not
resume one on the Agent's behalf, because the Agents name continuation
differently and a "most recent" choice can reach another role's conversation.
A restarted Agent can consult Room records; its context remains its own
responsibility. A newly started Agent may report `idle` before its own setup is
finished; the resulting delivery is uncertain and must be inspected rather
than assumed delivered. See
[implementation status](status.md#open-work).

## Message and addressing

All messages remain visible in the Room's single append-only stream. The CLI
stores target role names in structured message content; the Record contract
does not gain recipient fields. The [CLI reference](../reference/cli.md)
defines how text mentions and `--to` targets are selected and
validated. If an unknown role is found in a persisted record, the runtime
reports an undeliverable target without changing the record or routing to
another role.

The intended wake carries new Room messages directly; reading more history is
the Agent's choice. This is not yet the shipped wake behavior: the current
prompt asks the recipient to read the Room by room and sequence. The Room
stream remains the shared history. Builder and reviewer work in the same Room; the
reviewer is a different agent instance. A shared ancestor Room can carry
coordination across branches, with the relevant roles configured there.

## Delivery and recovery

A local integration process observes Room appends and uses hc to start a role
terminal or deliver a prompt to it. hc events report terminal facts, not Room
appends. hc events are hints: on startup and reconnect the integration
scans Room records from its durable boundary, because no event stream
provides replay. The integration stores its delivery progress
outside the Room stream and kernel, keyed by workspace, Room, record sequence,
and target role. It persists delivery intents before moving its scan boundary;
each intent then tracks its own outcome. Re-scanning must not create a second
delivery for that key.
Room-role dispatch is serialized, with later messages pending while that role
is busy. A workspace lock admits one local dispatcher for its lifetime. A
separate delivery-state lock serializes each scan with manual operations; when
it is briefly busy, the dispatcher skips that scan and tries again on the next
iteration. Other lock errors remain failures. Manual operations may still
report a busy delivery-state lock and can be retried.

For each directed message, resolve the configured role and its panel. Currently
a missing panel is started with its configured command and the Room and record reference;
the integration checks hc's first-message result, since a created terminal does
not prove that task was sent. An existing ready panel receives the reference
through hc. A busy panel leaves the message pending. Never force input into a
blocked or unready panel. hc readiness and reported status vary by agent; a tool
without reliable status cannot be assumed idle or done. When readiness cannot
be established, the delivery remains pending or is escalated.

hc 0.1.18 accepts an atomic `--if idle` guard but has no `--if done`. For a
panel reporting `done`, the current adapter rechecks its status and then sends
without `--force`; the check and send are not atomic. A concurrent actor could
start work in that gap. A blocked panel is still refused by hc. This remains
an open reliability limit, not an exactly-once or exclusive-use guarantee.

The runtime distinguishes pending, submission in progress, submitted, and
uncertain delivery. A successful terminal submission is not proof that the
agent completed the work. A matching Room reply can move a submitted delivery
to completed, but the reply remains an unverified claim: a signature and
reference do not authenticate the author. Replies cannot complete pending or
uncertain deliveries, so they cannot suppress work before submission or
resolve an ambiguous submission. The agent records its result or blocker in the
Room and references the triggering record; inspect that evidence when deciding
whether the claimed work is complete.
On interruption, refresh hc's terminal snapshot and inspect the Room before
deciding what to do. hc event sequence numbers restart and missed events yield
a snapshot, not a complete replay. A spawn or send timeout can mean the action
already happened: reconcile the panel and Room first. If delivery or an external
effect remains ambiguous, expose it for resolution instead of blindly starting
another agent or repeating the action. Exactly-once execution is not promised.

## First validation

Create a Room containing a builder and an independent reviewer backed by two
different agent tools. The builder writes progress and @mentions the reviewer;
the reviewer reads that Room, records evidence and a verdict there, and asks
the builder to revise when needed. Verify roster discovery, per-Room panel
isolation, busy and unavailable targets, and no human relay. Include an agent
that does not report hc status and verify pending or escalation. Interrupt the
integration before and after submission and during a turn; show that pending
work is recovered or ambiguity is visible. Record actual runs in
[evaluations.md](../evaluations.md). Authentication and one-time trust prompts
must be resolved before unattended operation.
