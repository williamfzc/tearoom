---
type: index
title: Architecture
description: Target layer boundaries, persistence, hosted deployment, and the implementation gap from the current code.
tags: [index, architecture]
timestamp: 2026-08-30
---

# Architecture

Architecture explains how to realize the target in [product.md](../product.md);
it is not a second product fact source. For shipped behavior, use
[status.md](status.md). For executable proof, use
[../evaluations.md](../evaluations.md). The design discussion and migration
notes below are rationale or evidence, not proof that the target is shipped.

| Document | Concern |
|---|---|
| [layers.md](layers.md) | Package ownership, dependency direction, and the optional activation boundary |
| [persistence.md](persistence.md) | Room membership, immutable Event history, and backend persistence |
| [activation.md](activation.md) | Canonical technical design for optional, restart-safe offline Agent activation |
| [convergence.md](convergence.md) | Target `send`/`read` path and its explicit migration boundary |
| [hosted-plan.md](hosted-plan.md) | Hosted control plane around the unchanged coordination kernel |
| [status.md](status.md) | Sole implementation-gap source: what the code implements and what remains |
| [model-discussion.md](model-discussion.md) | Design rationale and migration evidence for the target Room/Event model; not shipped-behaviour proof |

The target short version is: `core` defines Room, Actor, Event, and Store;
persistence enforces durable membership and ordered history; transport carries
the Store over HTTP; the CLI presents the interaction surface; and an optional
application service may activate an offline Agent. The executable used the
older cursor, directed-visibility, and command-group model until the P0 clean
break deleted it; see [status.md](status.md) for the shipped boundary.
