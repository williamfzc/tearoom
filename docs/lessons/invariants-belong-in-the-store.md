---
type: lesson
title: Invariants belong under the Store interface
description: A rule enforced only by one client disappears as soon as a second client arrives.
resource: internal/store/file.go
tags: [lesson, invariants, layering]
timestamp: 2026-08-28
---

# Invariants belong under the Store interface

## What happened

An earlier design kept coordination transitions in the CLI. Its tests passed because every test used that same client. A second client called Store directly and bypassed the rule.

## The rule

If a statement must be true of the Room rather than of one interface, enforce it under `core.Store`.

The current Store owns:

- Actor admission and recipient validation;
- cursor freshness;
- sequence assignment;
- idempotency-key uniqueness;
- append-only persistence;
- visibility filtering.

The HTTP server delegates to this boundary, so a raw HTTP caller and the CLI receive the same result.

## Guarded by

- Store tests for stale writes, duplicate retries, hidden Events, and concurrent writers.
- HTTP tests proving typed stale conflicts survive transport.
- `scripts/session-smoke.sh`, which sends a stale request through raw HTTP and expects 409.

## Related

- [../architecture/layers.md](../architecture/layers.md)
- [../domain/concepts.md](../domain/concepts.md)
