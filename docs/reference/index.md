---
type: index
title: Reference
description: The authoritative CLI, HTTP, exit, and storage contracts.
tags: [index, reference, contract]
timestamp: 2026-11-07
---

# Reference

This directory is the machine-facing contract. [product.md](../product.md)
is the sole product fact source;
[architecture/status.md](../architecture/status.md) is the implementation-gap
source; and [../evaluations.md](../evaluations.md) is the proof source.
Current implementation details do not belong in this tree.

| Document | Contract |
|---|---|
| [cli.md](cli.md) | The only command contract |
| [exit-codes.md](exit-codes.md) | Machine-readable outcomes |
| [storage-layout.md](storage-layout.md) | Workspace storage and the one-time format migration |
| [agent-integration.md](agent-integration.md) | Agent roles, hc panels, readiness, and handoffs |

Agents and Agent products should consume only these contracts. Human text
output may improve without changing the protocol. Current implementation
details belong exclusively in [status.md](../architecture/status.md).
