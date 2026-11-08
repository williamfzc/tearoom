---
type: lesson
title: Borrowed from consensus systems
description: Serialize Room facts once and make uncertain retries safe without turning every append into consensus.
resource: rust/src/store.rs
tags: [lesson, concurrency, consensus, design]
timestamp: 2026-09-09
---

# Borrowed from consensus systems

Tearoom is not a consensus implementation. It borrows one useful design posture: restrict the caller's freedom until correct behaviour is the easy behaviour.

## One ordered history

A Room owns one increasing sequence. Concurrent Events do not need globally synchronized wall clocks; the Store assigns their Room order under one lock.

## Freshness is protocol-specific

The v0.1 implementation requires every append to name the Room head it observed
and refuses the append when the head moved. That rule proved that stale
assumptions can be surfaced, but it also rejects unrelated concurrent
contributions and couples ordinary writing to hidden client progress.

The adopted product therefore does not make freshness a default append
condition. A protocol that truly requires compare-and-append may request an
explicit conditional capability and must define what state it protects. The
general lesson is to make a real precondition atomic at its owning boundary,
not to impose one precondition on every Event.

## Idempotent effects

A transport error can leave a caller unsure whether its Event landed. An
append may therefore carry a caller-chosen key. Retrying identical content with
that key returns the original Event; using the key for different content is
refused. Appends without a key remain valid and create distinct Events.

## Shared rules below every client

Room membership, authenticated authorship, sequence assignment, append-only
persistence, and any supported idempotency rule belong below every client.
Event visibility follows Room membership rather than a per-Event Store filter.

## What was not copied

Tearoom does not implement leader election, quorum replication, terms, or distributed consensus. The current Room is a single serialized log. Those mechanisms solve a different failure boundary and would be unjustified until the Store itself is replicated.

## Related

- [Invariants belong under Store](invariants-belong-in-the-store.md)
- [Cross-process locking](cross-process-locking.md)
- [../domain/concepts.md](../domain/concepts.md)
