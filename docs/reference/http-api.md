---
type: reference
title: HTTP API
description: Authoritative target HTTP protocol for Room interaction.
resource: internal/transport/server.go
tags: [reference, contract, http, api]
timestamp: 2026-08-30
---

# HTTP API

This is the only Room HTTP contract. It keeps Room history visible to every
member and leaves observation boundaries to callers. Implementation status
belongs in [status.md](../architecture/status.md).

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/room` | Read Room identity |
| `GET` | `/api/actors` | List admitted Actors |
| `POST` | `/api/actors` | Join or refresh an Actor |
| `GET` | `/api/events?after=12&limit=20&wait=30` | Read the complete Room history after a caller boundary, optionally waiting |
| `POST` | `/api/events` | Append one Event |

The target read boundary is the `after` query value. The server does not save, advance, or acknowledge it. A positive `wait` holds the request until a later Event exists or the wait elapses. An authenticated server may require an Actor for admission and authorship, but membership does not filter Event bodies.

## Append body

```json
{
  "actor": "alice",
  "to": ["bob"],
  "content": "review worktree A"
}
```

The server assigns the Event's ordered position and time. `to` is public addressing metadata; it is not private visibility and does not itself request activation. The target kernel does not require `type`, `caused_by`, an expected cursor, or an idempotency key for an ordinary append. Content schema and any conditional append remain open application decisions.

## Activation boundary

Activation is not part of the Room API. A separate service may observe an append, record an operational delivery attempt, and send a reference such as `(room, sequence, target Actor)` to a launcher. The Runtime then reads the Room through this API. Delivery state, retry, and restart recovery must not become read state or alter Event visibility.

Hosted identity and authorization are defined in
[hosted-identity.md](../domain/hosted-identity.md). No legacy request body or
endpoint alias is part of this contract.
