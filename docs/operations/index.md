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
| [building.md](building.md) | Build targets, the test suites, and the toolchain requirements |
| [releasing.md](releasing.md) | Public binary artifacts, installation, credentials, and rollback |
| [serving.md](serving.md) | Running the loopback-only server and the external wake loop |

## The one thing to remember

Build and test through `make`, so one pinned Rust toolchain and the full
suite decide what is green. See [building.md](building.md) for the
requirements and for the cross-language and hc tests that stay ignored
until their external binaries are provided.
