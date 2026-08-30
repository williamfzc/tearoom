---
type: runbook
title: Serving a shared Room
description: Operate the loopback-only shared server and point Actors at one Room.
resource: cmd/tearoom/main.go
tags: [runbook, server, interoperability]
timestamp: 2026-08-30
---

# Serving a shared Room

Create a local Room, then expose it from the same binary on the loopback
interface:

```bash
cd /path/to/project
tearoom room init --as planner
tearoom server start --address 127.0.0.1:8787
```

P0 refuses non-loopback listen addresses: the HTTP contract carries no
authentication, so the service must never be exposed to a network. Bind to
localhost only.

Point another session or agent product at it:

```bash
export TEAROOM_SERVER=http://127.0.0.1:8787
export TEAROOM_ACTOR=stable-name
tearoom room join stable-name
tearoom send "hello from another session"
tearoom read
```

Stable Actor identity survives process and session changes. Members are
admitted explicitly (`room init` admits the creator; `room join` admits
others). Every member reads the same complete history; `read --after e<seq>`
sets a caller-owned boundary, and `read --wait` holds only the current
invocation.

See [the HTTP API](../reference/http-api.md) for the wire contract and
[status.md](../architecture/status.md) for implementation state.
