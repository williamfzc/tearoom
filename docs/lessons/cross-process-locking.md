---
type: lesson
title: Test concurrency with processes, and prove the test can fail
description: Dense Room order must be verified with independent processes, not only goroutines.
resource: internal/store/concurrency_test.go
tags: [lesson, concurrency, testing]
timestamp: 2026-08-30
---

# Test concurrency with processes, and prove the test can fail

## The requirement

Several Agent products and sessions can write one Room. Every accepted append
must receive one unique dense sequence, even when independent processes race.
Ordinary target-model appends do not compete for an Actor checkpoint or require
all but one writer to fail.

## Why goroutines are not enough

Goroutines share one process. A process-local mutex could satisfy them while independent `tearoom` processes still race. The meaningful test launches separate OS processes against one `.tearoom` directory.

## The boundary

The file Store must hold `flock` across the complete sequence-assignment and
append operation. Idempotency resolution, when enabled, belongs inside the same
boundary.

The current v0.1 test launches ten processes on cursor zero and requires one
success plus nine stale conflicts. That proves the lock crosses process
boundaries, but it encodes the superseded mandatory-freshness contract. The
target test must accept all ten ordinary appends and prove a dense sequence with
no loss or duplication.

## Windows gap

The current Windows implementation has no safe cross-process lock. Concurrent use is unsupported there until a real primitive replaces the no-op.

## Related

- [../reference/storage-layout.md](../reference/storage-layout.md)
- [../domain/boundaries.md](../domain/boundaries.md)
