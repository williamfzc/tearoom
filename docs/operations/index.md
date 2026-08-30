---
type: index
title: Operations
description: Non-normative build, test, and deployment state during the clean break.
tags: [index, operations]
timestamp: 2026-08-30
---

# Operations

Operations records how to inspect or operate the current executable while it is
being replaced. It is not a source of
product facts or target contracts: consult [product.md](../product.md) for
scope, [architecture/status.md](../architecture/status.md) for implementation
gaps, and [../evaluations.md](../evaluations.md) for proof. Do not copy commands
from operations into product or integration guidance; the old surface is
deleted at the P0 clean break.

| Document | Concern |
|---|---|
| [building.md](building.md) | Build targets, the test suites, and two toolchain traps |
| [serving.md](serving.md) | Running the server and pointing Actors at one Room |
| [deploy.md](deploy.md) | Deploying the hosted server free: Render + Neon |
| [p0-room-cutover.md](p0-room-cutover.md) | The executable P0 clean-break cutover contract |

## The one thing to remember

Build with `make`, not bare `go build`. See [building.md](building.md) for why a
directly built binary can abort at startup.
