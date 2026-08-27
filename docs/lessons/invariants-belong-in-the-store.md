---
type: lesson
title: Invariants belong under the store interface
description: A gate rule implemented in the CLI let the browser console settle a gate while leaving its thread blocked forever.
resource: internal/store/file.go
tags: [lesson, invariants, layering, gates]
timestamp: 2026-08-27
---

# Invariants belong under the store interface

## What happened

The rule "opening a gate blocks its thread, deciding it unblocks the thread" was
implemented in the CLI, immediately after the store call. Every test passed, because
every test drove the CLI.

Then the browser console got an Approve button. It called `DecideGate` directly, as any
client would. The gate was marked approved and the thread stayed `blocked` — forever.

That is the exact silent stall this product exists to surface, shipped by the tool
meant to reveal it.

## Why the tests missed it

The test suite and the only client were the same thing. A rule enforced by the caller
is only tested by callers that remember it, and there was nothing to remember it wrong
until a second client existed.

## The fix

Not duplicating the rule in the frontend. The transition moved inside `CreateGate` and
`DecideGate`, under the same lock as the write, below the `Store` interface. Any client
that can reach the store now gets the invariant whether it remembers to or not.

A second bug surfaced immediately once the first was fixed, caught by a new test:
approval was assigning the thread to the human who approved it. The work now resumes as
the agent that **asked**. A human pressing a button is not volunteering to do the work,
and a board that says otherwise is lying about who is responsible.

## The rule

If a statement is true of the workspace rather than of a particular UI, it belongs
beneath the store interface.

A second client is the test that finds out. This applies directly to the planned MCP
adapter, which will be a third client and must not need to reimplement anything.

## Guarded by

- `TestGateBlocksAndUnblocksItsThread`, `TestRejectedGateReopensThread` in `internal/store`
- Five assertions in `scripts/smoke.sh` covering both approve and reject through the CLI

## Related

- [../architecture/layers.md](../architecture/layers.md) — the layering rule this reinforces
- [../domain/concepts.md](../domain/concepts.md) — the gate definition it protects
