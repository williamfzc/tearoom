---
type: index
title: Domain
description: Tearoom's three target entities and the rules that govern them.
tags: [index, domain]
timestamp: 2026-08-30
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
| [concepts.md](concepts.md) | Target Room, Actor, Event vocabulary and append contract |
| [problem.md](problem.md) | The failures the Room prevents and the optional activation gap |
| [boundaries.md](boundaries.md) | Authoritative failure and concurrency behavior |
| [agent-experience.md](agent-experience.md) | Target pure-read experience and the boundary with optional activation |
| [harness-workflow.md](harness-workflow.md) | Supporting multi-round handoff story |
| [security.md](security.md) | Membership visibility, trust boundaries, and required hardening |
| [hosted-identity.md](hosted-identity.md) | Supporting hosted identity and control-plane boundary |
| [open-questions.md](open-questions.md) | Decisions deliberately left unsettled |
