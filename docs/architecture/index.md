---
type: index
title: Architecture
description: Layer boundaries and the converged command and protocol surfaces.
tags: [index, architecture]
timestamp: 2026-08-28
---

# Architecture

| Document | Concern |
|---|---|
| [layers.md](layers.md) | Package ownership and dependency direction |
| [convergence.md](convergence.md) | Why all application actions reduce to Events |
| [status.md](status.md) | What exists and what remains deliberately absent |

The short version: `core` defines Room, Actor, Event, and Store; `store` enforces persistence invariants; `transport` carries Store over HTTP; `cli` presents the same operations; `cmd/tearoom` is the single process entry point.
