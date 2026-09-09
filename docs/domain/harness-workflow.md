---
type: domain
title: Harness workflow
description: The single cross-product continuation story expressed as ordinary Room Events rather than a kernel workflow.
tags: [domain, protocol, harness, user-story]
timestamp: 2026-09-09
---

# Harness workflow

## Target user story

A person or system moves ongoing work between processes or Agent products.
Actor A appends work; Actor B pulls the Room history through the CLI, continues,
and appends a result; Actor A or Actor C pulls that result and can continue
again. No human rewrites or relays Event content between products.

A human participant is an ordinary Actor in the same loop, not a separate user
story. The story is useful only if it survives process termination and a later
process recovers from an explicitly chosen boundary.

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
- A participant may be offline. An external operator or Agent product starts
  it and supplies the Room connection; the participant then uses ordinary
  `read` and `send`. Append itself never implies a wake.
- A process outcome is not a collaboration outcome. Only appended Events express what the group decided or produced.

## Target evaluation

The target evaluation is E3 in [../evaluations.md](../evaluations.md). It runs at
least two products through the four-step write, pull, write, pull loop, stops
one process between steps, and has a later process replay history before
contributing. It checks that no human copies Event content and no saved Room
cursor, private Event filter, mandatory type, causal field, or stale-write
precondition is needed.

Starting an offline participant was adjudicated out of product scope on
2026-09-09. An external operator or Agent product starts it and supplies the
Room connection; the participant then uses ordinary `read` and `send`.

## Why no new entities

A task, claim, artifact, verdict, round, or handoff is a projection or content convention over Room, Actor, and Event. A runner, activation ledger, or workflow engine belongs outside the Room if a later experiment proves that one is needed.
