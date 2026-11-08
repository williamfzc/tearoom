---
type: index
title: Tearoom knowledge base
description: Entry point to Tearoom's product design, implementation status, contracts, and evidence.
tags: [index]
timestamp: 2026-11-07
---

# Tearoom documentation

[**product.md**](product.md) is the sole source of product facts: scope, the
three concepts, invariants, refusals, and the completion boundary. This index
only routes readers; it does not add another product contract.

## Source hierarchy

Use one source for each kind of claim:

- [product.md](product.md) is the product fact source.
- [architecture/status.md](architecture/status.md) is the implementation-gap
  source: what the code ships and what remains open.
- [evaluations.md](evaluations.md) is the proof source: which executable checks
  support a story, plus the hand-run evidence record.
- [reference/](reference/index.md) is the CLI, HTTP, and machine-facing
  contract. It does not redefine product scope or implementation status.
- [log.md](log.md) is historical and retirement evidence only. It is not a
  normative entry point.

## Where to go

| Path | What lives there |
|---|---|
| [product.md](product.md) | Authoritative product: the three concepts, invariants, refusals, and proof standard |
| [evaluations.md](evaluations.md) | Proof source: acceptance journeys, evaluations, and hand-run evidence |
| [domain/](domain/index.md) | Room and Record concepts, boundary behaviour, and trust model |
| [architecture/](architecture/index.md) | Layer ownership, persistence, and the implementation gap |
| [reference/](reference/index.md) | The CLI, HTTP, exit, storage, and Agent integration contracts |
| [operations/](operations/index.md) | Non-normative build and release state; read only after status |
| [lessons/](lessons/index.md) | Durable lessons behind the current design |
| [log.md](log.md) | Historical decisions, retirements, and migration evidence; not a specification |

## Reading order

1. [product.md](product.md) — product facts
2. [domain/concepts.md](domain/concepts.md) — Room and Record behaviour
3. [architecture/status.md](architecture/status.md) — implementation gap
4. [evaluations.md](evaluations.md) — proof and hand-run evidence
5. [reference/index.md](reference/index.md) — machine-facing contracts
6. [operations/index.md](operations/index.md) — operational state, only when needed

Only after this path should a reader open the lessons or historical log.
Product intent in the docs does not by itself establish that a feature is
implemented; current implementation status is recorded in
[architecture/status.md](architecture/status.md).
