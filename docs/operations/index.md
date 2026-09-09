---
type: index
title: Operations
description: Non-normative build, test, and serving state for the shipped executable.
tags: [index, operations]
timestamp: 2026-08-30
---

# Operations

Operations records how to build, test, and serve the shipped executable. It is
not a source of product facts or target contracts: consult
[product.md](../product.md) for scope, [architecture/status.md](../architecture/status.md)
for implementation state, and [../evaluations.md](../evaluations.md) for proof.

| Document | Concern |
|---|---|
| [building.md](building.md) | Build targets, the test suites, and the toolchain traps |
| [releasing.md](releasing.md) | Public binary artifacts, installation, credentials, and rollback |
| [serving.md](serving.md) | Running the loopback-only server and pointing Actors at one Room |

## The one thing to remember

Build and test with `make`, not bare `go build`. See [building.md](building.md)
for why a directly built binary or test executable can abort at startup on
this machine.
