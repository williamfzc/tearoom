---
type: reference
title: HTTP API
description: Authoritative HTTP protocol for the served Room.
resource: internal/transport/server.go
tags: [reference, contract, http, api]
timestamp: 2026-09-09
---

# HTTP API

This is the only Room HTTP contract. It keeps Room history visible to every
member and leaves observation boundaries to callers. The service is served on
the loopback interface only in P0. Implementation status belongs in
[status.md](../architecture/status.md).

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/room` | Read Room identity |
| `GET` | `/api/actors` | List admitted Actors |
| `POST` | `/api/actors` | Explicitly join an Actor |
| `GET` | `/api/events?after=42&limit=100&wait=30` | Read Events after a caller boundary, optionally waiting |
| `POST` | `/api/events` | Append one Event |

- `GET /api/events` returns a plain JSON Event array — there is no Feed and no
  cursor. `after` is the exclusive caller-owned boundary; the server never
  saves or advances it. `wait` is an integer number of seconds: the request
  holds until an Event past `after` exists or the wait elapses, then returns
  an array (possibly empty). This is invocation-scoped transport behavior, not
  a durable Feed/ReadWait abstraction; it creates no cursor or delivery state.
  The server caps one wait invocation at 60 seconds; callers needing a longer
  wait must loop externally with the same caller-owned boundary. An empty array
  is "nothing new", not an error.
- `POST /api/actors` is explicit join under loopback/OS trust. The body is
  `{"id":"<actor>"}`.
- `POST /api/events` appends one Event. The body carries only `to` and
  `content`; any other field — in particular the removed fields of the old
  wire format — is rejected with 400.
- Errors use the unified envelope `{"error":{"code":"...","message":"..."}}`.
  `409` means an idempotency-key conflict and nothing else; the CLI maps it to
  exit code 1.

## Headers

| Header | Used by | Meaning |
|---|---|---|
| `X-Tearoom-Actor` | events routes | the read/send Actor; required |
| `Idempotency-Key` | append | optional retry identity for the append |

## Append body

```json
{
  "to": ["bob"],
  "content": "review worktree A"
}
```

`content` is arbitrary JSON; plain text is sent as a JSON string by the CLI.
The server assigns the Event's ordered position and time and returns the Event
as one JSON object. `to` is public addressing metadata; it is not private
visibility and does not itself request activation. An ordinary append requires
no `type`, `caused_by`, expected cursor, or idempotency key.

## Minimal curl journey

Start the server in one terminal from a directory that already contains a
Room:

```bash
tearoom server start --address 127.0.0.1:8787
```

In a second terminal, join an Actor and exchange one Event. The server carries
the Actor identity in `X-Tearoom-Actor`; the join body is explicit and the
append body contains only `to` and `content`:

```bash
curl -sS -X POST http://127.0.0.1:8787/api/actors \
  -H 'Content-Type: application/json' \
  -d '{"id":"bob"}'

curl -sS -X POST http://127.0.0.1:8787/api/events \
  -H 'Content-Type: application/json' \
  -H 'X-Tearoom-Actor: planner' \
  -H 'Idempotency-Key: review-1' \
  -d '{"to":["bob"],"content":{"request":"review"}}'

curl -sS 'http://127.0.0.1:8787/api/events?after=0&limit=100' \
  -H 'X-Tearoom-Actor: bob'
```

The response to the last request is a JSON Event array. A wait request such as
`/api/events?after=1&wait=1` returns an empty array if nothing new arrives;
repeat it in the caller when more than the 60-second server cap is needed.

## Idempotency

An append with `Idempotency-Key` is matched per `(room, actor, key)`. An equal
retry (same compacted content and normalized `to`) returns the original Event;
the same key with different content returns `409`. No key: every append
creates a distinct Event. The key is never stored as an Event field.

## Delivery and deployment boundary

Starting an absent Agent is outside this API. Once an external operator or
Agent product starts and configures it, the Agent reads and appends through
these same Room endpoints. Process state must not become read state or alter
Event visibility. There is no share, snapshot, or continuation endpoint.

Remote identity and authorization are deferred. They extend this same service:
local bootstrap and remote login resolve through one authorization path before
these Room operations. Actor names are not secrets. No separate remote Room API,
legacy request body, or endpoint alias is part of the contract.
