---
type: index
title: Architecture
description: Layer boundaries, dependency rules, and the reasoning behind a single-language codebase.
tags: [index, architecture]
timestamp: 2026-08-27
---

# Architecture

How the code is arranged and which direction dependencies are allowed to run.

| Document | Concern |
|---|---|
| [layers.md](layers.md) | The four layers, what each owns, and the one-way rule |
| [isomorphic-go.md](isomorphic-go.md) | Why the console is Go compiled to WebAssembly rather than JavaScript |
| [status.md](status.md) | What exists at M0 and what is deliberately missing |

## The short version

`core` defines the concepts and the `Store` contract and depends on nothing.
`store` and `view` sit above it: one persists, one derives. `resolve` assembles raw
reads into derived state. `cli` and `transport` are the outermost layer, where users,
networks and processes live.

Nothing reaches upward. When two peers need the same truth, it moves down into a
layer that owns it — see [layers.md](layers.md) for the rule and
[../lessons/invariants-belong-in-the-store.md](../lessons/invariants-belong-in-the-store.md)
for what it cost to learn.
