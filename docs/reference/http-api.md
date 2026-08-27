---
type: reference
title: HTTP API
description: Endpoints exposed by tea-server, including the resolved board the console renders.
resource: internal/transport/server.go
tags: [reference, contract, http, api]
timestamp: 2026-08-27
---

# HTTP API

Served by `tea-server`. Every response is JSON. Errors carry a status code that maps
onto the CLI's [exit codes](exit-codes.md), so a remote conflict is still a conflict.

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/workspace` | Workspace identity |
| GET | `/api/board` | The whole workspace, already resolved |
| GET | `/api/agents` | All agents |
| POST | `/api/agents` | Register or refresh an agent (`{ID, Role}`) |
| GET | `/api/threads` | All threads |
| POST | `/api/threads` | Create a thread (`{Agent, Title, Body}`) |
| GET | `/api/threads/{id}` | One raw thread |
| GET | `/api/threads/{id}/context` | Compacted context for that thread |
| POST | `/api/threads/{id}/events` | Append an event (`{Agent, Body, Status, Evidence}`) |
| GET | `/api/leases` | All leases |
| POST | `/api/leases` | Claim (`{Agent, Resource, Thread, TTL}`) |
| POST | `/api/leases/release` | Release (`{Agent, Resource}`) |
| GET | `/api/gates` | All gates |
| POST | `/api/gates` | Open a gate (`{Agent, Reason, Thread}`) |
| GET | `/api/gates/{id}` | One gate |
| POST | `/api/gates/{id}/decide` | Settle a gate (`{State, By, Note}`) |
| GET | `/api/ledger` | Audit trail, newest first |
| POST | `/api/ledger` | Append a ledger entry |

When `--web` is given, anything not under `/api/` serves the console assets.

## Why `/api/board` exists

This endpoint is the reason there is a server at all rather than just a CLI. It runs
`view.Build` server-side and ships one already-resolved board, so the console never
reimplements "who is stuck" in another language.

The client decodes it back into the same Go struct the server encoded. See
[../architecture/isomorphic-go.md](../architecture/isomorphic-go.md).

## Note on the transport client

`transport.Client` implements `core.Store`, so code above the interface cannot tell a
local directory from a remote server. Setting `TEA_SERVER` changes nothing about how
`tea` behaves — see [../operations/serving.md](../operations/serving.md).

## Related

- [cli.md](cli.md) — the same operations from a terminal
- [exit-codes.md](exit-codes.md) — how HTTP statuses map to exit codes
