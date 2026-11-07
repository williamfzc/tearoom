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
| [persistence.md](persistence.md) | The room tree, per-room streams, and the one-time format migration |
| [status.md](status.md) | Sole implementation-gap source: what the code implements and what remains |

The short version: `core` defines Room, Record, Notification, and `Store`;
persistence holds the tree and the streams in one workspace database;
transport carries the minimal local HTTP face; the CLI presents the command
surface. Waking absent workers is external — a waker reads `watch` output and
starts the next worker, which continues through the ordinary CLI path.
