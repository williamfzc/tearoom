---
type: index
title: Architecture
description: Layer ownership, persistence, and the implementation status of the shipped kernel.
tags: [index, architecture]
timestamp: 2026-09-09
---

# Architecture

Architecture explains how the target in [product.md](../product.md) is
realized; it is not a second product fact source. For executable proof, use
[../evaluations.md](../evaluations.md).

| Document | Concern |
|---|---|
| [layers.md](layers.md) | Package ownership and dependency direction |
| [persistence.md](persistence.md) | Room membership, immutable Event history, and backend persistence |
| [status.md](status.md) | Sole implementation-gap source: what the code implements and what remains |

The short version: `core` defines Room, Actor, Event, and `Store`;
persistence enforces durable membership and ordered history; transport carries
the Store over HTTP; the CLI presents the interaction surface. Starting an
absent Agent lives outside the repository; once running, it continues through
the ordinary CLI `read` and `send` path.
