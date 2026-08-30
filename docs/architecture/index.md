---
type: index
title: Architecture
description: Layer boundaries, persistence, hosted deployment, and the migration from the current implementation to the minimum Room/Event model.
tags: [index, architecture]
timestamp: 2026-08-30
---

# Architecture

| Document | Concern |
|---|---|
| [layers.md](layers.md) | Package ownership, dependency direction, and the optional activation boundary |
| [persistence.md](persistence.md) | Room membership, immutable Event history, and backend persistence |
| [convergence.md](convergence.md) | Target `send`/`read` path and its explicit migration boundary |
| [hosted-plan.md](hosted-plan.md) | Hosted control plane around the unchanged coordination kernel |
| [status.md](status.md) | What the code implements and what the design baseline still leaves open |
| [model-discussion.md](model-discussion.md) | Canonical design rationale: minimum Room/Event, membership visibility, no implicit cursor, and optional activation |

The target short version is: `core` defines Room, Actor, Event, and Store;
persistence enforces durable membership and ordered history; transport carries
the Store over HTTP; the CLI presents the interaction surface; and an optional
application service may activate an offline Agent. The current executable still
uses the older cursor, directed-visibility, and command-group model; see
[status.md](status.md) before treating the target as shipped.
