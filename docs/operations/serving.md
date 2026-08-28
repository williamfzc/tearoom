---
type: runbook
title: Serving a shared Room
description: Expose one Room so independent sessions and agent products can exchange Events.
resource: cmd/tearoom/main.go
tags: [runbook, server, sessions, interoperability]
timestamp: 2026-08-28
---

# Serving a shared Room

Create a local Room, then expose it from the same binary:

```bash
tearoom room init --name project
tearoom server start --address 127.0.0.1:8787 --directory /path/to/project
```

Point another session or agent product at it:

```bash
export TEAROOM_SERVER=http://127.0.0.1:8787
export TEAROOM_ACTOR=stable-name
tearoom event list
tearoom event send "hello from another session"
```

The stable Actor identity and its local cursor survive process and session changes. Client cursor files live in the user's Tearoom configuration directory, so a remote client does not need a local Room checkout. A different product may use the HTTP API directly and sees the same order, freshness, idempotency, and visibility rules.

The service has no browser console and no authentication. Bind to localhost or a trusted network only.

See [the HTTP API](../reference/http-api.md) for the wire contract.
