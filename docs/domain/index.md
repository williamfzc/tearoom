---
type: index
title: Domain
description: Tearoom's three entities and the rules that govern them.
tags: [index, domain]
timestamp: 2026-09-09
---

# Domain

The minimum vocabulary is Room, Actor, and Event. Room membership determines
visibility; `to` addresses an Event without making it private. Application
protocols may add envelopes and projections, but they do not automatically
become kernel entities. [Product definition](../product.md) owns the product
facts; [architecture/status.md](../architecture/status.md) owns implementation
gaps; [evaluations.md](../evaluations.md) owns proof.

| Document | Concern |
|---|---|
| [concepts.md](concepts.md) | The three entities, the write/read contract, boundary behaviour, trust model, and the failures the Room prevents |
| [harness-workflow.md](harness-workflow.md) | The single cross-product continuation story |
| [open-questions.md](open-questions.md) | Decisions deliberately left unsettled |
