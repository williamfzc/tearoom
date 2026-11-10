---
type: index
title: Architecture
description: Layer ownership, persistence, and the implementation status of the room-tree kernel.
tags: [index, architecture]
timestamp: 2026-11-07
---

# Architecture

Architecture explains how the target in [product.md](../product.md) is
realized; it is not a second product fact source. For executable proof, use
[../evaluations.md](../evaluations.md).

| Document | Concern |
|---|---|
| [layers.md](layers.md) | Package ownership and dependency direction |
| [collaboration.md](collaboration.md) | Roles, hc panel routing, and recoverable delivery |
| [persistence.md](persistence.md) | The room tree, per-room streams, and the one-time format migration |
| [status.md](status.md) | Sole implementation-gap source: what the code implements and what remains |

The short version: `core` defines Room, Record, and the Store seam;
persistence holds the tree and streams in workspace files; the CLI is the
automation interface, with an optional loopback browser view. The [collaboration integration](collaboration.md) connects
Room records to hc.
