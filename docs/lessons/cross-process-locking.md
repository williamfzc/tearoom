---
type: lesson
title: Test concurrency with processes, and prove the test can fail
description: Goroutine-based tests cannot detect a missing flock; real processes can, and the test was verified by removing the lock.
resource: internal/store/concurrency_test.go
tags: [lesson, concurrency, testing, leases]
timestamp: 2026-08-27
---

# Test concurrency with processes, and prove the test can fail

## The requirement

Two agents in separate processes calling `claim` at the same instant must not both
succeed. This is the single correctness property the product exists for: without it,
both read "free", both write "held", and a silent overwrite follows.

## Why goroutines are not enough

Agents are separate OS processes. A test that spawns goroutines shares one address
space, so it exercises Go's mutexes but says nothing about whether two `tea` processes
serialise. Mutual exclusion across processes needs `flock`, and only real processes can
demonstrate it.

So the test compiles a helper binary and launches twelve of them against one workspace,
all racing for the same resource.

## The part that mattered

A passing concurrency test is weak evidence — it may pass because the race never
occurred. So the lock was deliberately removed and the test re-run.

Result: all twelve processes acquired the same lease simultaneously. The test failed
loudly, which proved it was capable of failing. The lock was restored and it passed
again.

An untested test is an assumption. Breaking it on purpose is the cheapest way to find
out whether it has teeth.

## Known gap

`flock` is unavailable on Windows, where the lock degrades to a no-op. That is a real
limitation, recorded in the code at `internal/store/lock_other.go` rather than left to
be discovered. Concurrent use on Windows is unsafe today.

## Related

- [../domain/concepts.md](../domain/concepts.md) — the lease invariant this protects
- [../domain/boundaries.md](../domain/boundaries.md) — what a losing claim is told
- [../operations/building.md](../operations/building.md) — running the race suite
