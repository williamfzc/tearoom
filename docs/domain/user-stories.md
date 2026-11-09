---
type: domain
title: Collaboration journeys
description: User-facing journeys and acceptance criteria for Room collaboration and hc-backed Agent delivery.
tags: [user-stories, collaboration, acceptance]
timestamp: 2026-11-07
---

# Collaboration journeys

These stories define how people and Agents use Rooms together. The product
model lives in [product.md](../product.md); this document owns the story
triggers and observable acceptance criteria. Proof status and evidence live in
the [evaluation registry](../evaluations.md).

The current design places builder and reviewer roles in the same project
Room. This supersedes the earlier review boundary that required a separate
review Room; that requirement is withdrawn by the latest user decision.
Separate child Rooms can coordinate through their common parent Room.

## Shared context and continuity

### S1: One Agent completes one task

**When** a person writes a goal and acceptance conditions in a Room, **an
Agent can** read that Room, do the work, and leave progress and evidence there.

**Accept when:** the Agent completes the task with zero human relays and a
fresh reader of that Room can identify the goal, result, evidence, and next
step.

### S2: Builder and reviewer share a project Room

**When** a builder works in a project Room, **a reviewer can** join the same
Room's conversation, read the shared records, and write a pass or fail verdict
with evidence there. Builder and reviewer are distinct roles with independent
hc panels. The builder never certifies its own work.

**Accept when:** the reviewer needs no human summary, cites commands and
outputs available in the project Room, and the verdict is preserved in that
Room's history.

### S3: A new process resumes later

**When** the original process has ended and a day has passed, **a new Agent
can** read the relevant Rooms and continue, optionally using a different tool.

**Accept when:** from Room records alone it answers the goal, prior work,
whether acceptance is met, and the next step.

## Room conversation and routing

### S4: A message reaches its intended collaborator

**When** a Room record explicitly mentions a collaborator, **the collaboration
runtime can** use hc to notify or start that Agent and provide the Room and
record to read.

**Accept when:** the intended Agent receives the handoff without a human
relay, reads the Room as the source of context, and writes its result to the
Room named by the handoff.

### S5: An Agent discovers who can help

**When** an Agent needs a reviewer or another skill, **it can** list the roles
configured in the Room and their descriptions, then choose whom to mention.

**Accept when:** the selection can be made from the displayed information,
and a missing or unavailable role is reported clearly. This
listing helps route work; it does not grant or restrict permission to read or
write a Room.

### S6: A Room behaves like a group conversation

**Proposed default:** when someone writes a normal message, **all collaborators
can** read it in the shared Room history. Active collaborators may observe new
messages; an ordinary message does not start every offline Agent. An explicit
mention directs a wake to its named collaborator. The interaction needs
validation with real Agents; the shipped syntax is in the
[CLI reference](../reference/cli.md).

**Accept when:** broadcast visibility is preserved, mentions reach their
targets, and ordinary discussion does not create an unbounded wake loop.

### S7: Review and revision continue in one Room

**When** a builder asks for review, **the runtime can** start or notify a
reviewer through hc in the same project Room. The reviewer and builder share
the Room history and address one another directly with mentions. A failed
verdict can mention the builder to continue work in that Room.

**Accept when:** the reviewer is independent, the evidence and verdict live in
the shared Room, and a requested revision can continue without a human moving
context between separate Rooms.

### S9: Separate Rooms coordinate through their common parent

**When** work is split across child Rooms, **the Agents can** coordinate by
writing shared decisions and handoffs in their common parent Room. Each child
Room keeps its own execution history; the parent carries the conversation
needed by the branches together.

**Accept when:** an Agent in either child can read the parent Room and its own
Room to understand shared decisions and continue, without writing directly
into a sibling Room.

## Reusable roles and Room setup

### S10: Reuse role definitions across Rooms

**When** a person or Agent creates a Room, **they can** choose from reusable
role definitions, such as builder or reviewer, and add those roles to the
Room's collaboration setup. Each Room-role assignment has its own hc panel so
the role's running Agent context is independent within that Room.

**Accept when:** one role definition can be reused in multiple Rooms, each
Room's configured roles can be listed, and activity in one Room's role panel
does not silently become another Room's panel history. Role configuration is
for collaboration and routing, not access control or signature validation.

### S11: Choose a collaborator by role and agent type

**When** a role has to be configured or its availability investigated,
**a person or Agent can** inspect which Agent implementation it uses.

**Accept when:** the role's purpose is distinguishable from the concrete
Agent type that will run it, and a missing or unavailable implementation is
reported clearly rather than silently substituted. Role discovery and choosing
a collaborator are covered by [S5](#s5-an-agent-discovers-who-can-help).

## Delivery and recovery

### S8: Pending work survives runtime interruption

**When** the runtime or an Agent stops after a message is written, **a later
runtime can** find the pending handoff from durable state and continue delivery.

**Accept when:** a message is not silently lost; after interruption, the
runtime reconciles the Room record with observed delivery and execution before
retrying. It must not blindly repeat effects whose completion is uncertain,
and must show unresolved ambiguity to a person. This does not assume exactly
once execution of external side effects.

## Current design boundaries

- A project Room can host several roles in one shared conversation. Each
  configured Room-role has an independent hc panel. A common parent Room can
  carry shared conversation across child Rooms.
- Room history is the durable shared context; hc provides Agent terminals,
  prompt delivery, and runtime observations. Delivery and recovery belong to
  the [integration](../architecture/collaboration.md).
- Reusable role definitions and Room role assignments support discovery and
  routing; they do not authenticate a signature or create a permission boundary.
- Normal messages are visible to roles in the Room. The proposed default is
  not to start every offline role; a mention expresses an intended wake.
- Implementation status and remaining evidence are tracked in
  [implementation status](../architecture/status.md).

