---
type: reference
title: HTTP API
description: Authoritative HTTP protocol for the minimal local face of the room tree.
resource: internal/transport/server.go
tags: [reference, contract, http, api]
timestamp: 2026-11-07
---

# HTTP API

This is the only Room HTTP contract. It serves the room tree of one workspace
over plain JSON with no authentication paths — it is the loopback face of the
same trust domain as the `.tearoom` directory. Implementation status belongs
in [status.md](../architecture/status.md).

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/rooms` | List every Room (name, parent, creation time) |
| `GET` | `/api/events?room=<name>&after=<seq>` | Read one Room's records after an exclusive boundary |
| `POST` | `/api/events` | Append one Record |

- `GET /api/events` returns a plain JSON Record array — there is no envelope
  and no cursor. `after` is the exclusive caller-owned boundary; the server
  never saves or advances it. An empty array is "nothing new", not an error.
  An unknown room answers the unified error envelope with 404.
- `POST /api/events` appends one Record. The body carries `room` and
  `content`; any other field is rejected with 400.
- Errors use the unified envelope `{"error":{"code":"...","message":"..."}}`.
  `409` means an idempotency-key conflict and nothing else; the CLI maps it
  to exit code 1.

## Headers

| Header | Used by | Meaning |
|---|---|---|
| `X-Tearoom-Author` | append | the signature label; optional, stored verbatim |
| `Idempotency-Key` | append | optional retry identity, scoped per (room, signature, key) |

There is no `Authorization` header and no credential route: the loopback
server has no authentication paths.

## Append body

```json
{
  "room": "implementation",
  "content": "review worktree A"
}
```

`content` is arbitrary JSON; plain text is sent as a JSON string by the CLI.
The server assigns the record's ordered position and time and returns the
Record as one JSON object.

## Minimal curl journey

Start the server in one terminal from a directory that already contains a
workspace:

```bash
tearoom server start --address 127.0.0.1:8787
```

In a second terminal, append one record and read the stream back:

```bash
curl -sS -X POST http://127.0.0.1:8787/api/events \
  -H 'Content-Type: application/json' \
  -H 'X-Tearoom-Author: codex' \
  -H 'Idempotency-Key: review-1' \
  -d '{"room":"implementation","content":{"request":"review"}}'

curl -sS 'http://127.0.0.1:8787/api/events?room=implementation&after=0'
```

The response to the last request is a JSON Record array.

## Idempotency

An append with `Idempotency-Key` is matched per (room, signature, key). An
equal retry (same compacted content) returns the original Record; the same
key with different content returns `409`. No key: every append creates a
distinct Record. The key is never stored as a Record field.

## Deployment boundary

The server binds the loopback interface only and serves one workspace. There
is no hosted multi-room service, no owner token, and no TLS profile: exposing
this server beyond one trusted machine is a misdeployment. Starting an absent
Agent is outside this API; an external waker reads notifications and starts
the next worker, which then uses these same endpoints.
