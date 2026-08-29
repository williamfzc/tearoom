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
| `GET` | `/api/events?actor=a&after=12&limit=20&wait=30` | Read a visible Feed, optionally waiting |
| `POST` | `/api/events` | Append one `core.AppendInput` |

## Server-held wait

`GET /api/events` accepts `wait`, a non-negative number of seconds. With `wait=0` (or omitted) the request returns immediately, like the file Store. With a positive `wait`, the server holds the request until a visible Event appears after the cursor and returns it at once; if `wait` elapses with nothing new, it returns an empty Feed with the requested cursor so the client can ask again. `wait` is capped at 60 seconds; longer waits are sliced into repeated requests.

A held request costs the server nothing while the room is quiet: appends through the server wake it directly, and an idle fallback check uses the Store's cheap change probe rather than a full read. A waiting Actor still sees only Events visible to it.

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
