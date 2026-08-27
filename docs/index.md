---
type: index
title: Tearoom knowledge base
description: Entry point to Tearoom's durable documentation, organised by concern.
tags: [index]
timestamp: 2026-08-27
---

# Tearoom documentation

Coordination for several agents working in one repository. Five concepts, ten
commands, one language.

This tree holds knowledge with durable value: decisions, domain definitions, public
contracts, boundaries, runbooks, and lessons that cost something to learn. Temporary
motion is left out on purpose — see [AGENTS.md](../AGENTS.md).

Each concept lives in exactly one file, identified by its path. Everything else
links here rather than restating it.

## Where to go

| Directory | What lives there |
|---|---|
| [architecture/](architecture/index.md) | Layers, dependency direction, and why the whole thing is one language |
| [domain/](domain/index.md) | The five concepts and the rules that govern them |
| [reference/](reference/index.md) | Public contracts: the CLI surface and the HTTP API |
| [operations/](operations/index.md) | Building, testing, and serving the console |
| [lessons/](lessons/index.md) | Mistakes worth not repeating, with the evidence that produced them |

## Reading order for a newcomer

1. [domain/concepts.md](domain/concepts.md) — what a thread, lease and gate are.
2. [architecture/layers.md](architecture/layers.md) — where code is allowed to live.
3. [reference/cli.md](reference/cli.md) — the command surface.
4. [lessons/index.md](lessons/index.md) — why some things look the way they do.

## Status

M0, working end to end: CLI, HTTP server, and a browser console compiled from the
same Go source. See [architecture/status.md](architecture/status.md) for what is
built and what is deliberately absent.
