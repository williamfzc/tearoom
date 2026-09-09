---
type: domain
title: Harness workflow
description: A multi-round agent handoff expressed as ordinary Room Events rather than a kernel workflow.
tags: [domain, protocol, harness, user-story]
timestamp: 2026-08-30
---

# Harness workflow

## Target user story

A harness operator starts a planner, generator, and evaluator from different products in one Room. The planner proposes work; the generator reports an attempt; the evaluator reports a decision; the planner or generator continues through one or more revision rounds; and a human may intervene before the work closes. No process relays the entire context by hand: every participant can replay the same ordered Room history.

The kernel does not define plan, generation, evaluation, revision, or closure. They are conventions in Event content or projections over the log. The story is useful only if it still works when one process ends and a later process reads the Room from an explicitly chosen boundary.

## Protocol convention

An application may use content such as:

| Round | Actor | Content convention |
|---|---|---|
| 1 | planner | propose the task and intended plan |
| 2 | generator | report an implementation attempt |
| 3 | evaluator | report evidence and a verdict |
| 4 | planner or generator | revise the plan or implementation |
| 5 | evaluator | report the next verdict |
| 6 | planner | close after the evidence is sufficient |

These labels are examples, not kernel fields. The minimal Event still consists of an ordered position, author, time, optional public `to` metadata, and content. A future application may define a richer content schema, but it must not make `type`, `caused_by`, or a workflow entity mandatory in the Room.

## State invariants

- Every accepted Event remains in the one ordered Room history.
- Every Room member can inspect the same history, including earlier rounds.
- A caller chooses its read boundary; the Room does not advance or store one for the Actor.
- A response may mention an earlier Event in its content, but causal links are an application convention rather than a kernel field.
- A participant may be offline. That is ordinary replay; delivery is a paste or an external script, and no wake is implied by append.
- A process outcome is not a collaboration outcome. Only appended Events express what the group decided or produced.

## Target evaluation

The target evaluation should run at least two products through three rounds, stop one process between rounds, let a human append an intervention, and have a later process replay the complete history before contributing. It must check that no saved Room cursor, private Event filter, mandatory type, causal field, or stale-write precondition is needed.

Delivery to an offline participant was adjudicated out of product scope on 2026-09-09. A waiting online process is not evidence that an offline Actor can be woken safely.

## Why no new entities

A task, claim, artifact, verdict, round, or handoff is a projection or content convention over Room, Actor, and Event. A runner, activation ledger, or workflow engine belongs outside the Room if a later experiment proves that one is needed.
