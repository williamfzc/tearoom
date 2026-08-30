---
type: reference
title: HTTP API
description: Target Room HTTP protocol and the current implementation boundary.
resource: internal/transport/server.go
tags: [reference, contract, http, api]
timestamp: 2026-08-30
---

# HTTP API

This document separates the target protocol from the currently shipped server. The target protocol keeps Room history public to its members and leaves observation boundaries to callers.

## Target endpoints

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/room` | Read Room identity |
| `GET` | `/api/actors` | List admitted Actors |
| `POST` | `/api/actors` | Join or refresh an Actor |
| `GET` | `/api/events?after=12&limit=20&wait=30` | Read the complete Room history after a caller boundary, optionally waiting |
| `POST` | `/api/events` | Append one Event |

The target read boundary is the `after` query value. The server does not save, advance, or acknowledge it. A positive `wait` holds the request until a later Event exists or the wait elapses. An authenticated server may require an Actor for admission and authorship, but membership does not filter Event bodies.

## Target append body

```json
{
  "actor": "alice",
  "to": ["bob"],
  "content": "review worktree A"
}
```

The server assigns the Event's ordered position and time. `to` is public addressing metadata; it is not private visibility and does not itself request activation. The target kernel does not require `type`, `caused_by`, an expected cursor, or an idempotency key for an ordinary append. Content schema and any conditional append remain open application decisions.

## Target activation boundary

Activation is not part of the Room API. A separate service may observe an append, record an operational delivery attempt, and send a reference such as `(room, sequence, target Actor)` to a launcher. The Runtime then reads the Room through this API. Delivery state, retry, and restart recovery must not become read state or alter Event visibility.

## Current implementation boundary

The shipped server still exposes the same broad paths but implements the older Store contract: reads accept `actor`, current visibility filtering, and saved-client-cursor usage; appends require `expected` and `key` and accept `type`, `data`, `to`, and `caused_by`; stale writes return HTTP 409. Those fields and semantics are compatibility behavior, not target requirements.

The current append shape is retained here for migration reference:

```json
{
  "actor": "alice",
  "expected": 12,
  "type": "handoff.requested",
  "body": "review worktree A",
  "data": {"path": "../project-review"},
  "to": ["bob"],
  "caused_by": "e10",
  "key": "handoff-a-12"
}
```

The hosted server currently authenticates credentials before delegating to this compatibility API. Its development admin bearer, capability checks, and route prefix remain implementation details; the target hosted identity contract is in [../domain/hosted-identity.md](../domain/hosted-identity.md).
