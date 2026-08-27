---
type: index
title: Operations
description: Building, testing, and serving Tearoom, including the toolchain traps that cost time.
tags: [index, operations]
timestamp: 2026-08-27
---

# Operations

| Document | Concern |
|---|---|
| [building.md](building.md) | Build targets, the test suites, and two toolchain traps |
| [serving.md](serving.md) | Running the server and pointing agents at it |

## The one thing to remember

Build with `make`, not bare `go build`. See [building.md](building.md) for why a
directly built binary can abort at startup.
