---
type: lesson
title: Test concurrency with processes, and prove the test can fail
description: Cross-process ordering depends on flock and must be verified with independent processes.
resource: internal/store/concurrency_test.go
tags: [lesson, concurrency, testing]
timestamp: 2026-08-28
---

# Test concurrency with processes, and prove the test can fail

## The requirement

Several Agent products and sessions can write one Room. Given the same expected cursor, exactly one process may append the next sequence; every other writer must receive a stale conflict.

## Why goroutines are not enough

Goroutines share one process. A process-local mutex could satisfy them while independent `tearoom` processes still race. The meaningful test launches separate OS processes against one `.tearoom` directory.

## The boundary

The file Store holds `flock` across the complete operation: read the head, inspect idempotency keys, compare the expected cursor, assign the sequence, append, and sync.

`internal/store/concurrency_test.go` launches ten processes on cursor zero and requires one success, nine stale conflicts, and exactly one Event on disk.

## Windows gap

The current Windows implementation has no safe cross-process lock. Concurrent use is unsupported there until a real primitive replaces the no-op.

## Related

- [../reference/storage-layout.md](../reference/storage-layout.md)
- [../domain/boundaries.md](../domain/boundaries.md)
