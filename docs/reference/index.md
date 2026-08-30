---
type: index
title: Reference
description: The authoritative target CLI, HTTP, exit, and storage contracts.
tags: [index, reference, contract]
timestamp: 2026-08-30
---

# Reference

This directory is the target machine-facing contract. [product.md](../product.md)
is the sole product fact source; [architecture/status.md](../architecture/status.md)
is the implementation-gap source; and [../evaluations.md](../evaluations.md) is
the proof source. Current implementation details do not belong in this tree.

| Document | Contract |
|---|---|
| [cli.md](cli.md) | The only Room command contract |
| [http-api.md](http-api.md) | The only Room HTTP protocol |
| [exit-codes.md](exit-codes.md) | Target machine-readable outcomes |
| [storage-layout.md](storage-layout.md) | Target Room storage and operational-state boundary |

Agents and Agent products should consume only these contracts. Human text
output may improve without changing the protocol. Current implementation
details belong exclusively in [status.md](../architecture/status.md).
