---
type: lesson
title: Borrowed from consensus systems
description: Restrict caller freedom, order facts once, and reject work based on an obsolete view.
resource: internal/store/file.go
tags: [lesson, concurrency, consensus, design]
timestamp: 2026-08-28
---

# Borrowed from consensus systems

Tearoom is not a consensus implementation. It borrows one useful design posture: restrict the caller's freedom until correct behaviour is the easy behaviour.

## One ordered history

A Room owns one increasing sequence. Concurrent Events do not need globally synchronized wall clocks; the Store assigns their Room order under one lock.

## Freshness at commit

An Actor reads cursor `N` and appends with `expected=N`. If another Event moved the Room, the Store refuses the append and reports the current cursor. The caller must read the delta before deciding whether its intended Event remains true.

This prevents the observed failure where an agent, working from an old snapshot, recorded a confident statement that another Actor had already made false.

## Idempotent effects

A transport error can leave a caller unsure whether its Event landed. Every append therefore carries a caller-chosen key. Retrying identical content with that key returns the original Event; using the key for different content is refused.

## Rules below every client

Freshness, idempotency, order, and visibility are Store rules. Implementing any one only in the CLI would let raw HTTP or a future client bypass it.

## What was not copied

Tearoom does not implement leader election, quorum replication, terms, or distributed consensus. The current Room is a single serialized log. Those mechanisms solve a different failure boundary and would be unjustified until the Store itself is replicated.

## Related

- [Invariants belong under Store](invariants-belong-in-the-store.md)
- [Cross-process locking](cross-process-locking.md)
- [../domain/concepts.md](../domain/concepts.md)
