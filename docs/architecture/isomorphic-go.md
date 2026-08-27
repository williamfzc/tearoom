---
type: decision
title: One language for both ends
description: Why the browser console is Go compiled to WebAssembly instead of a JavaScript frontend.
resource: cmd/tea-web/main.go
tags: [architecture, decision, wasm, frontend]
timestamp: 2026-08-27
---

# One language for both ends

The console is Go, compiled to WebAssembly. There is no JavaScript in this project
beyond the loader Go itself ships.

## The decision

Whether an agent is stuck, whether a thread is waiting on a human, and how urgent
something is are all judgements. They live in `internal/view` as pure functions.

If the terminal and the browser each implemented those judgements, they would drift.
Not immediately — the first divergence would be a rounding difference in how idle time
is displayed, and the second would be a thread the CLI calls stalled and the console
calls healthy. At that point the tool built to reveal confusion has become a source of
it.

So `view.Build` runs once. `tea status` renders its result as text; the console
renders the same struct as DOM. The server sends an already-resolved board over the
wire, and the console decodes it into the same Go type it was encoded from.

## What this buys

The `Store` contract is implemented twice — by the local file store and by the HTTP
client — and callers above the interface cannot tell which one they hold. That is what
makes `TEA_SERVER=...` change nothing about how `tea` behaves, and it is what lets the
console compile the same `view` package against a `Client` instead of a `File`.

## What it costs

The wasm bundle is roughly 8.7 MB. For a tool a team self-hosts on its own machine
that is acceptable; for a public page it would not be. Recorded here so the tradeoff
is a decision rather than an accident.

Frontend tests need a JavaScript runtime, and Go's wasm exec wrapper cannot inherit a
large environment — see [../operations/building.md](../operations/building.md) for the
`env -i` invocation this forces.

## Related

- [layers.md](layers.md) — the layering this decision depends on
- [../lessons/console-repaint.md](../lessons/console-repaint.md) — a bug that only appears when a frontend is real
- [../operations/building.md](../operations/building.md) — how to build and test the wasm target
