---
type: index
title: Tearoom knowledge base
description: Entry point to Tearoom's durable documentation, organised by concern.
tags: [index]
timestamp: 2026-08-27
---

# Tearoom documentation

Coordination for several agents working in one place. Five concepts, one language, and a
command surface still converging on ten.

[**product.md**](product.md) defines the scope and is authoritative over every other
document here, including this one.

This tree holds knowledge with durable value: decisions, domain definitions, public
contracts, boundaries, runbooks, and lessons that cost something to learn. Temporary
motion is left out on purpose — see [AGENTS.md](../AGENTS.md).

Each concept lives in exactly one file, identified by its path. Everything else
links here rather than restating it.

## Where to go

| Path | What lives there |
|---|---|
| [product.md](product.md) | What Tearoom is, who it serves, what it refuses, how a proposal is judged |
| [architecture/](architecture/index.md) | Layers, dependency direction, and why the whole thing is one language |
| [domain/](domain/index.md) | The five concepts and the rules that govern them |
| [reference/](reference/index.md) | Public contracts: the CLI surface and the HTTP API |
| [operations/](operations/index.md) | Building, testing, and serving the console |
| [lessons/](lessons/index.md) | Mistakes worth not repeating, with the evidence that produced them |

## Reading order for a newcomer

1. [product.md](product.md) — what this is and what it refuses to be.
2. [domain/concepts.md](domain/concepts.md) — what a thread, lease and gate are.
3. [architecture/layers.md](architecture/layers.md) — where code is allowed to live.
4. [reference/cli.md](reference/cli.md) — the command surface.
5. [lessons/index.md](lessons/index.md) — why some things look the way they do.

## Status

M0, working end to end: CLI, HTTP server, and a browser console compiled from the
same Go source. Two known gaps are significant — stale writes are accepted, and the
attention axis is unbuilt. See [architecture/status.md](architecture/status.md) for the
full list, separated from what is deliberately absent.

## Status

M0, working end to end: CLI, HTTP server, and a browser console compiled from the
same Go source. See [architecture/status.md](architecture/status.md) for what is
built and what is deliberately absent.
