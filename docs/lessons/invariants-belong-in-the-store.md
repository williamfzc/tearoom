---
type: lesson
title: Invariants belong under the Store interface
description: A rule enforced only by one client disappears as soon as a second client arrives.
resource: internal/store/file.go
tags: [lesson, invariants, layering]
timestamp: 2026-08-30
---

# Invariants belong under the Store interface

## What happened

An earlier design kept coordination transitions in the CLI. Its tests passed because every test used that same client. A second client called Store directly and bypassed the rule.

## The rule

If a statement must be true of the Room rather than of one interface, enforce it under `core.Store`.

The current Store owns:

- Room membership and addressed-member validation;
- authenticated Actor attribution;
- sequence assignment;
- append-only persistence;
- idempotency-key uniqueness when the capability is used.

All members read the same Room history, so the target Store has no directed
visibility filter. Read boundaries belong to callers, and ordinary append has
no implicit cursor-freshness rule.

The HTTP server delegates to this boundary, so a raw HTTP caller and the CLI receive the same result.

## Guarded by

- Existing Store and HTTP tests guard the v0.1 behavior.
- The migration must replace hidden-Event and mandatory stale-write cases with
  membership visibility, pure reads, and concurrent ordinary append cases.
- Duplicate-retry tests remain relevant if explicit append idempotency survives
  the migration.

## Related

- [../architecture/layers.md](../architecture/layers.md)
- [../domain/concepts.md](../domain/concepts.md)
