---
type: architecture
title: Room collaboration delivery
description: Proposed local role configuration, hc panel routing, and recoverable handoff contract.
tags: [architecture, collaboration, hc, delivery]
timestamp: 2026-11-08
---

# Room collaboration delivery

This is the proposed first local integration for the product and
[user journeys](../domain/user-stories.md). It is not implemented or proven.
The [implementation status](status.md) records what ships. The kernel owns
Room history; this integration owns configuration, delivery and recovery.

## Roles and Room panels

Define a reusable role with a stable name, description, agent command or hc
template, working-directory setting, role instructions, and the readiness and
input behavior needed by that agent. Instructions supplied through an hc
template are an initial message, not a guaranteed system prompt. Credentials
remain with the agent environment; Room messages and role descriptions do not
store secrets.

Room creation selects role definitions. The resulting roster is discoverable
through the CLI and is routing configuration, not permission to read or write.
One Room-role binding identifies one logical panel and at most one active hc
terminal at a time. Different Rooms using the same definition have separate
terminals and conversations. A panel may be created on demand; a lost terminal
can be replaced without changing the Room-role identity. A CLI view should
show the role name, description, agent type and observed availability, including
unknown or blocked; it must not claim that an absent or unready panel is ready.
The exact configuration file and commands need a first runnable slice.

## Message and addressing

All messages remain visible in the Room's single append-only stream. The CLI
accepts an explicit @role and stores target role names as structured message
content alongside the text; the Record and Notification contracts do not gain
recipient fields. The integration routes only explicit, machine-readable
targets, not guesses from prose or quoted text. Sending without a target is a
broadcast in shared history and does not start offline agents. `@all` may
notify existing panels but does not start every offline role by default. Exact
input escaping and the structured content encoding need a boundary test before
they become a CLI contract. If an unknown role is detected before append, the
CLI reports it; if found in a persisted record, the runtime reports an
undeliverable target without changing the record or routing to another role.

The recipient reads the Room record by room and sequence, rather than treating
the prompt as shared history. Builder and reviewer work in the same Room; the
reviewer is a different agent instance. Separate Rooms coordinate in a shared
ancestor Room, with the relevant roles configured there.

## Delivery and recovery

A local integration process observes Room appends and uses hc to start a role
terminal or deliver a prompt to it. hc events report terminal facts, not Room
appends. Notifications are hints: on startup and reconnect the integration
also scans Room records from its durable boundary. The current `watch` stream
alone cannot provide replay. The integration stores its delivery progress
outside the Room stream and kernel, keyed by workspace, Room, record sequence,
and target role. It persists delivery intents before moving its scan boundary;
each intent then tracks its own outcome. Re-scanning must not create a second
delivery for that key.
Room-role dispatch is serialized, with later messages pending while that role
is busy. A dedicated workspace lock admits one local dispatcher; another
process must not dispatch until it holds that lock.

For each directed message, resolve the configured role and its panel. A missing
panel is started with its configured template and the Room and record reference;
the integration checks hc's first-message result, since a created terminal does
not prove that task was sent. An existing ready panel receives the reference
through hc. A busy panel leaves the message pending. Never force input into a
blocked or unready panel. hc readiness and reported status vary by agent; a tool
without reliable status cannot be assumed idle or done. When readiness cannot
be established, the delivery remains pending or is escalated.

The runtime distinguishes pending, submission in progress, submitted, and
uncertain delivery. A successful terminal submission is not proof that the
agent completed the work. The agent records its result or blocker in the Room
and references the triggering record; completion is judged from that evidence.
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
