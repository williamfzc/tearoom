---
type: index
title: Tearoom knowledge base
description: Entry point to Tearoom's product design, implementation status, contracts, and evidence.
tags: [index]
timestamp: 2026-08-30
---

# Tearoom documentation

Tearoom is a durable shared Room where stable Actors contribute to one ordered,
replayable history. An optional activation layer may wake an absent Agent, but
the Room remains useful without it.

[**product.md**](product.md) is authoritative over every other document, including this index and `README.md`.

## Where to go

| Path | What lives there |
|---|---|
| [product.md](product.md) | Authoritative target product, invariants, refusals, and proof standard |
| [competitive-analysis.md](competitive-analysis.md) | Market landscape, Tearoom's differentiation, and the near-term priorities it justifies |
| [evaluations.md](evaluations.md) | User stories, the evaluations that prove them, and the bidirectional map between the two |
| [domain/](domain/index.md) | Room, Actor, Event, and boundary behaviour |
| [architecture/](architecture/index.md) | Design rationale, layer ownership, and the target-to-implementation gap |
| [reference/](reference/index.md) | Target CLI and wire contracts, with legacy implementation differences named |
| [operations/](operations/index.md) | Building, testing, and serving a Room |
| [lessons/](lessons/index.md) | Durable lessons behind the current design |
| [log.md](log.md) | Decisions and structural changes |

## Reading order

1. [product.md](product.md)
2. [domain/concepts.md](domain/concepts.md)
3. [architecture/layers.md](architecture/layers.md)
4. [reference/cli.md](reference/cli.md)
5. [operations/building.md](operations/building.md)

Readers evaluating current behavior must also read
[architecture/status.md](architecture/status.md). Product adoption in the docs
does not imply that the Store, CLI, hosted service, demos, or Agent skill have
already migrated.
