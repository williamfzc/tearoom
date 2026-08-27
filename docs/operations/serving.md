---
type: runbook
title: Serving the console
description: Running tea-server, pointing agents at it, and what the console will and will not do.
resource: cmd/tea-server/main.go
tags: [runbook, server, console]
timestamp: 2026-08-27
---

# Serving the console

```bash
tea-server --addr :8787 --web ./web
# console on http://localhost:8787
```

Point agents at it and nothing else changes:

```bash
export TEA_SERVER=http://localhost:8787
tea claim "src/**" --ttl 15m
```

The CLI behaves identically against a local directory or a remote server, because
`transport.Client` implements the same `core.Store` interface the file store does. A
conflict raised remotely still exits 2 and still names the holder and expiry.

## What the console shows

Attention first, and loudest. A human should be able to answer "what needs me?" without
scanning healthy rows. When nothing is wrong it says so plainly instead of presenting a
dashboard to interpret.

## What the console will not do

Its only write actions are approve and reject. This is a deliberate limit: adding
messaging, assignment or editing would turn it into the chat application this project
exists to avoid becoming.

## Polling, not push

The board is polled every three seconds. A human glances at this a few times an hour, so
a websocket would buy nothing and cost reconnection logic on both ends.

The console only rebuilds when state actually changes — see
[../lessons/console-repaint.md](../lessons/console-repaint.md).

## Related

- [../reference/http-api.md](../reference/http-api.md) — the endpoints behind it
- [building.md](building.md) — producing the wasm bundle it needs
