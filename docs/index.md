---
type: index
title: Tearoom knowledge base
description: Entry point to Tearoom's product design, implementation status, contracts, and evidence.
tags: [index]
timestamp: 2026-08-30
---

# Tearoom documentation

[**product.md**](product.md) is the sole source of product facts: scope, target
model, invariants, refusals, and completion boundary. This index only routes
readers; it does not add another product contract.

## Source hierarchy

Use one source for each kind of claim:

- [product.md](product.md) is the product fact source.
- [architecture/status.md](architecture/status.md) is the implementation-gap
  source: what the code ships and what remains to migrate.
- [evaluations.md](evaluations.md) is the proof source: which executable checks
  support a story and which still exercise legacy behavior.
- [architecture/activation.md](architecture/activation.md) is the technical
  source for the optional offline-Agent control plane; it does not extend the
  product kernel.
- [reference/](reference/index.md) is the target CLI, HTTP, and machine-facing
  contract. It does not redefine product scope or current implementation status.
- [log.md](log.md) is historical and migration evidence only. It is not a
  normative entry point.

## Where to go

| Path | What lives there |
|---|---|
| [product.md](product.md) | Authoritative target product, invariants, refusals, and proof standard |
| [evaluations.md](evaluations.md) | Proof source: user stories and the evaluations that prove them |
| [competitive-analysis.md](competitive-analysis.md) | Supporting market evidence; it does not set product scope |
| [domain/](domain/index.md) | Room, Actor, Event, and boundary behaviour |
| [architecture/](architecture/index.md) | Design rationale, layer ownership, optional activation design, and the target-to-implementation gap |
| [reference/](reference/index.md) | The only target CLI, HTTP, exit, and storage contracts |
| [operations/](operations/index.md) | Non-normative build and deployment state; read only after status |
| [lessons/](lessons/index.md) | Durable lessons behind the current design |
| [log.md](log.md) | Historical decisions and migration evidence; not a specification |

## Reading order

1. [product.md](product.md) — target product facts
2. [domain/concepts.md](domain/concepts.md) — target Room, Actor, and Event vocabulary
3. [architecture/status.md](architecture/status.md) — implementation gap
4. [evaluations.md](evaluations.md) — proof and legacy-evidence boundary
5. [architecture/activation.md](architecture/activation.md) — optional P2
   technical design, only when activation is in scope
6. [reference/index.md](reference/index.md) — target machine-facing contracts
7. [operations/index.md](operations/index.md) — non-normative operational state, only when needed

Only after this path should a reader open rationale, lessons, or the historical
log. Product adoption in the docs does not imply that the Store, CLI, hosted
service, or demos have already migrated. The old Agent skill has been removed;
no executable integration guide exists until the target CLI lands.
