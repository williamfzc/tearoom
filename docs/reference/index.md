---
type: index
title: Reference
description: The authoritative CLI, exit-code, storage, and Agent integration contracts.
tags: [index, reference, contract]
timestamp: 2026-11-07
---

# Reference

This directory holds the machine-facing contracts. [product.md](../product.md)
is the sole product fact source;
[architecture/status.md](../architecture/status.md) is the implementation-gap
source; and [../evaluations.md](../evaluations.md) is the proof source.
Implementation details belong here only when they define a current external
contract; validation evidence belongs in [evaluations](../evaluations.md).

| Document | Contract |
|---|---|
| [cli.md](cli.md) | The only command contract |
| [app.md](app.md) | The optional desktop face |
| [exit-codes.md](exit-codes.md) | Machine-readable outcomes |
| [storage-layout.md](storage-layout.md) | Workspace storage and the one-time format migration |
| [agent-integration.md](agent-integration.md) | Agent setup, hc panels, Herdr visibility, and handoff limits |

The CLI page owns command names, flags, and command behavior. Other pages link
to it instead of restating that contract. Agents can consume these contracts;
human text output may improve without changing machine-readable behavior.
