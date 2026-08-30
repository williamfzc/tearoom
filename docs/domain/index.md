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
become kernel entities. See the [model rationale](../architecture/model-discussion.md)
for the target and migration gap.

| Document | Concern |
|---|---|
| [concepts.md](concepts.md) | The three entities, target Event shape, and append contract |
| [problem.md](problem.md) | The failures the Room prevents and the optional activation gap |
| [boundaries.md](boundaries.md) | Target boundaries and current compatibility behaviour |
| [agent-experience.md](agent-experience.md) | Target pure-read experience and the boundary with optional activation |
| [harness-workflow.md](harness-workflow.md) | User story: plan/generate/evaluate agents run one loop through a Room |
| [security.md](security.md) | Target membership visibility, current trust boundary, and required hardening |
| [hosted-identity.md](hosted-identity.md) | GitHub sign-in, Room ownership, and Agent credentials |
| [open-questions.md](open-questions.md) | Decisions deliberately left unsettled |
