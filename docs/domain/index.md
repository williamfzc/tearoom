---
type: index
title: Domain
description: Tearoom's concepts, collaboration stories, and open design questions.
tags: [index, domain]
timestamp: 2026-11-07
---

# Domain

The minimum vocabulary is Room, Record, and Notification. Rooms form a
single-parent tree; each Room owns one ordered record stream; a Notification
is the envelope emitted when a record lands. Signatures are unverified
labels. Protocols and conventions live in the records people write, not in
kernel entities. [Product definition](../product.md) owns the product facts;
[architecture/status.md](../architecture/status.md) owns implementation gaps;
[evaluations.md](../evaluations.md) owns proof.

| Document | Concern |
|---|---|
| [concepts.md](concepts.md) | The three concepts, the write/read contract, boundary behaviour, and trust model |
| [user-stories.md](user-stories.md) | User journeys and acceptance criteria for Room collaboration and hc delivery |
| [open-questions.md](open-questions.md) | Decisions deliberately left unsettled |
