---
type: domain
title: Harness workflow
description: User story: planner, generator, and evaluator agents run one plan/generate/evaluate loop through a Room.
tags: [domain, protocol, harness, user-story]
timestamp: 2026-08-29
---

# Harness workflow

## User story

A harness — planner, generator, and evaluator agents from different products — needs one durable channel to run an evaluation loop on a task. The story:

> As a harness operator, I start three agents — planner, generator, evaluator — in one Room. The planner proposes a plan, the generator waits for it and reports generated code, the evaluator waits and reports a verdict, the loop iterates until approval, and the planner closes the task. Nobody relays context by hand; every agent reads exactly what became visible since its own cursor.

This is the "task protocol outside the kernel" evidence item in [status.md](../architecture/status.md): if the loop runs on Room, Actor, Event alone, the three entities remain enough.

## The protocol

Application-level event types, not kernel entities:

| Type | Actor | Meaning |
|---|---|---|
| `plan.proposed` | planner | The task and the plan to implement it |
| `generation.reported` | generator | Code produced, chained to the plan or to a revision request |
| `evaluation.reported` | evaluator | Structured verdict in `data` (`score`, `verdict`), chained to the artifact under review |
| `note.whisper` | any | A directed note (`--to`) that stays private to its recipients |
| `task.completed` | planner | The loop closes, chained to the approved evaluation |

The rules of the road are exactly the product invariants:

- **Watch is the handoff.** Each agent blocks on `event watch` until the Room moves past its cursor, so a loop participant never polls and never misses a step.
- **`caused-by` is the chain.** Every step names the Event it responds to; the loop's history is one trace, replayable in order.
- **Directed Events keep reviews private.** A reviewer note targeted at the generator never appears in the planner's feed, but still advances the planner's cursor past it — the planner learns the Room moved, not why.
- **Stale writes are the resync signal.** A participant who acts on old knowledge is refused (exit 2) rather than recorded, then reads to the head and retries. The planner's close in the scenario lands this way on purpose.

## Executable scenario

[scripts/harness-demo.sh](../../scripts/harness-demo.sh) runs the story against the shipped binary and is part of `make check`. It proves:

1. one full plan → generate → evaluate → revise → approve → close loop, seven Events, no human relay;
2. the full `caused-by` trace e1→e2→e3→e4→e5 and the whisper chained to the approval;
3. the whisper reaches the generator and stays hidden from the planner;
4. the planner's stale close is refused, then succeeds after the list/resync pattern.

## Why not new entities

A task, a claim, an artifact, and a verdict are event types and projections, not core entities. The loop above is the product's "what it enables" claim ([product.md](../product.md)) in executable form: no workflow engine, scheduler, or DAG inside the kernel.

This story is **S2** in the [evaluation set](../evaluations.md); its proof is evaluation E5 (`harness-demo`).
