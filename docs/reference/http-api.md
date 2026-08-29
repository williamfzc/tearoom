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

## Hosted endpoints

`tearoom hosted serve` exposes the same event endpoints per Room under a tenant path, plus a management surface. One shared database hosts many Rooms; the event API is described above, with `/r/<room-id>/api` in place of `/api`.

| Method | Path | Purpose | Auth |
|---|---|---|---|
| `POST` | `/api/hosted/login` | Phase 1: answer the dev account for the admin bearer | admin bearer |
| `POST` | `/api/hosted/rooms` | Create a Room owned by the admin | admin bearer |
| `GET` | `/api/hosted/rooms` | List the admin's Rooms | admin bearer |
| `POST` | `/api/hosted/rooms/<id>/credentials` | Issue a credential (`actor`, `capabilities`, `ttl_seconds`); returns its token once | admin bearer |
| `DELETE` | `/api/hosted/rooms/<id>/credentials/<cred>` | Revoke a credential | admin bearer |
| `GET` | `/r/<room-id>/api/room` | Room identity | credential |
| `GET` | `/r/<room-id>/api/actors` | List Actors | credential, read |
| `POST` | `/r/<room-id>/api/actors` | Register one's own Actor | credential, any capability |
| `GET` | `/r/<room-id>/api/events` | Read a visible Feed | credential, read, own actor |
| `POST` | `/r/<room-id>/api/events` | Append one Event | credential, append, own actor |

The dev admin bearer is `TEAROOM_HOSTED_ADMIN` with `TEAROOM_HOSTED_DEV=1`; it will be replaced by the GitHub OAuth exchange (Phase 2). Clients use the ordinary environment: `TEAROOM_SERVER=http://host/r/<room-id>`, `TEAROOM_TOKEN=<credential>`.

## Hosted trust boundary

The hosted server authenticates every event-API request by Bearer credential before the coordination layer sees it. A credential is bound to one Room and one Actor; requests that declare another actor, or lack a capability, are refused with 403; missing, unknown, revoked, expired, or wrong-room credentials answer 401 — a token cannot probe which Rooms exist. Revocation takes effect on the next request. Registering one's own Actor needs any capability because joining is identity, not content.

Phase 1 still assumes TLS termination in front of the server, has no rate limiting, and uses a dev admin token for management routes; see [hosted-plan.md](../architecture/hosted-plan.md) and [security.md](../domain/security.md).

The plain `tearoom server start` remains unauthenticated: use it only on a trusted machine or network; recipient filtering is not a security boundary without authenticated Actor identity.
