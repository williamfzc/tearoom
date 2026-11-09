---
type: lesson
title: Invariants belong under the Store boundary
description: A rule enforced only by one client disappears as soon as a second client arrives.
resource: rust/src/store.rs
tags: [lesson, invariants, layering]
timestamp: 2026-09-09
---

# Invariants belong under the Store boundary

## What happened

An earlier design kept coordination transitions in the CLI. Its tests passed because every test used that same client. A second client called Store directly and bypassed the rule.

## The rule

If a statement must be true of the Room rather than of one client, enforce it under the Store boundary.

The current Store owns:

- room-parent validation (a child records an existing parent, and only once);
- sequence assignment;
- append-only persistence;
- idempotency-key uniqueness when the capability is used.

Signatures are labels: the Store validates no identity and filters no record.
Read boundaries belong to callers, and ordinary append has no
implicit cursor-freshness rule.

Every caller of the Store receives the same rules, regardless of its interface.

## Guarded by

- Store and CLI tests guard the adopted Room contract.
- The completed clean break replaced hidden-Event and mandatory stale-write
  cases with membership visibility, pure reads, and concurrent ordinary append
  cases.
- Duplicate-retry tests guard the optional append idempotency capability.

## Related

- [../architecture/layers.md](../architecture/layers.md)
- [../domain/concepts.md](../domain/concepts.md)
