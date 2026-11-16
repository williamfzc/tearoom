---
type: reference
title: LAN face contract
description: The opt-in HTTP face of the collaboration service — pairing, endpoints, token model, threat model, and limits.
resource: src/lan.rs
tags: [reference, lan, contract, network]
timestamp: 2026-11-16
---

# LAN face contract

The LAN face is the opt-in network view of one workspace, hosted inside the
per-user collaboration service (see [collaboration.md](../architecture/collaboration.md)).
A paired phone reads the Room tree and appends records; the records enter the
same Store streams as every other writer, so the dispatcher treats a phone
send exactly like a CLI send and the [delivery boundary](cli.md#delivery-boundary)
is unchanged. The [desktop app](app.md) remains the local face; this page owns
the network one.

## Enabling and pairing

The face is off until a pairing is requested. `tearoom collab pair`
[--port <port>] starts the service if needed, turns the face on at `port`
(default 7761, override with `TEAROOM_LAN_PORT`), and prints one address and
one pairing code. The code is six digits, single-use, valid for five minutes,
and refused after five rejected attempts; a new `collab pair` replaces it.
The face keeps running (and keeps its tokens) across service restarts;
`service status` reports its state under `lan`.

The phone presents the address and the code once and receives a bearer token:
32 bytes of OS entropy, stored on the phone, kept on the Mac only as a
SHA-256 hash. Each token is bound to the workspace it paired with and can see
nothing else. There is no token revocation command yet; removing
`lan.json` from the service state directory disables the face and forgets
every token.

## Endpoints

All requests and responses are JSON. Every response carries permissive CORS
headers so the app's webview can call the face cross-origin; the token, not
the origin, is the authority.

```text
POST /v1/pair   {"code": "<6 digits>"}
              → {"token": "<64 hex>", "workspace": "<name>"}
GET  /v1/rooms                → {"rooms": [Room…]}
GET  /v1/records?room=<room>&after=<e seq>
                              → {"records": [Record…]}
POST /v1/send   {"room": "<room>", "text": "<text>",
                 "author": "<signature>", "key": "<idempotency key>",
                 "to": ["<role>", …]}   (room, text, key required)
              → {"record": Record}
OPTIONS *                      → 204 with CORS headers
```

`rooms` and `records` return the same shapes as the desktop app's commands
([app.md](app.md)); `room` accepts an id or a name, and `after` takes either
`e<seq>` or a plain integer. The send path shells out to the CLI `send`
command with `--idempotency-key`, so retries with the same `key` and content
replay the original record and conflicting content is refused — a phone that
loses connectivity resends safely. Errors are
`{"error": "<message>"}` with 400 (malformed or refused request), 401
(missing or unknown token), 403 (rejected pairing), 404 (unknown room or
route), 405 (wrong method), 408/413/431 (request-shape limits: headers
16 KiB, bodies 1 MiB), or 500 (local failure).

## Reading contract

The face offers no push channel. A phone polls `records` with its own
`after` boundary — the same cadence the desktop UI uses (2.5 s), honoring
the caller-owned read boundary of [product.md](../product.md#invariants).
The store keeps no cursor for the face or for anyone.

## Threat model and limits

The face speaks plain HTTP with a bearer token, and that is a deliberate
scope: it is meant for the user's own trusted LAN. Anyone who obtains the
token can read and write the paired workspace, so the token must not leave
the user's devices; the pairing code protects the exchange for five minutes
on one network. TLS is the named follow-up if the face ever leaves the
trusted network; reopening that decision belongs in [../log.md](../log.md).

Refused on purpose (see [../log.md](../log.md) for the dated decision):

- A second append implementation inside the face — sends go through the CLI
  `send` command, keeping composition, roster rules, and activation under
  one owner.
- A push/SSE channel — the polling contract already exists and is shared
  with the desktop face.
- mDNS discovery — the printed address is the v1 match mechanism; reopen
  when manual entry demonstrably frictions real use.
- The live role panel and terminal drawer — no PTY semantics exist over this
  face; the phone reads rooms, not terminals.
