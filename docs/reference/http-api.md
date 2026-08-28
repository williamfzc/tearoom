---
type: reference
title: HTTP API
description: The Store contract exposed by tearoom server start.
resource: internal/transport/server.go
tags: [reference, contract, http, api]
timestamp: 2026-08-28
---

# HTTP API

`tearoom server start` exposes one Room. Responses are JSON and the remote client implements the same `core.Store` contract as the local file store.

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/room` | Read Room identity |
| `GET` | `/api/actors` | List Actors |
| `POST` | `/api/actors` | Join or refresh an Actor with `{ID, Role}` |
| `GET` | `/api/events?actor=a&after=12&limit=20` | Read a visible Feed |
| `POST` | `/api/events` | Append one `core.AppendInput` |

## Append body

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

`expected` and `key` are required. The cursor is checked and the next sequence assigned under the same Store lock. A stale request receives HTTP 409 with `kind: stale_write`, `expected`, and `actual`.

## Trust boundary

The current server has no authentication. Use it only on a trusted machine or network; recipient filtering is not a security boundary without authenticated Actor identity.
