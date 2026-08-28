---
type: index
title: Domain
description: The five concepts Tearoom is built from and the rules that govern them.
tags: [index, domain]
timestamp: 2026-08-27
---

# Domain

The vocabulary of the product. Five concepts, and a standing rule that a sixth must
prove the existing five cannot express it.

| Document | Concern |
|---|---|
| [concepts.md](concepts.md) | Workspace, agent, thread, lease, gate — definitions and invariants |
| [problem.md](problem.md) | The three failure modes this product exists to prevent |
| [boundaries.md](boundaries.md) | What happens at the edges: crashes, unreachable stores, deadlocks |
| [agent-experience.md](agent-experience.md) | Attention, time and context as budgets — the axis not yet built |
| [security.md](security.md) | What is stored, what is refused, and what revocation must guarantee |
| [open-questions.md](open-questions.md) | Decisions deliberately left unsettled |

## Why the count is fixed

Every added concept multiplies the states a human has to hold in their head while
scanning a board. The value of a small fixed vocabulary is that someone joining a
project does not have to learn its private dialect first.
