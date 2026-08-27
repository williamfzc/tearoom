---
type: decision
title: Open questions
description: Decisions deliberately left unsettled at M0, with what each one blocks and what would settle it.
tags: [domain, decision, open]
timestamp: 2026-08-27
---

# Open questions

Recorded rather than guessed. Each needs evidence, not an opinion.

## Workspace-to-repository binding

One workspace per repository, or per branch?

Per branch would let two agents work on unrelated branches without contending for the
same leases. It also multiplies workspaces and raises the question of what happens to
an in-flight thread when its branch is deleted.

Blocks: nothing at M0, since single-repo use works either way. Settle before multi-branch
usage becomes common.

## Default lease TTL

Too short interrupts a long edit; too long slows recovery after a crash.

Needs measurement against how long real agent edits actually take. Guessing a number
here would bake in a wrong default that later becomes hard to change.

## Open-sourcing the client

Leaning yes for the CLI, no for the server. The CLI is the surface agents integrate
against, and adoption depends on it being inspectable. The hosted store is where the
paid value would sit.

## Cold-start distribution

Open-source traction, or the MCP ecosystem? These imply different first milestones and
should not both be pursued at once.

## Related

- [concepts.md](concepts.md) — the definitions these questions would refine
- [../architecture/status.md](../architecture/status.md) — what is built so far
