---
type: runbook
title: Serving a shared Room
description: Operate the current shared server while the CLI and Event contract migrate to the adopted design.
resource: cmd/tearoom/main.go
tags: [runbook, server, sessions, interoperability]
timestamp: 2026-08-30
---

# Serving a shared Room

The shipped server still exposes the v0.1 contract. Create a local Room, then
expose it from the same binary:

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

Stable Actor identity survives process and session changes. The current client
also resumes a local cursor and the current server enforces freshness and
directed visibility; those three behaviors are migration debt rather than the
adopted product contract. The target client will use explicit `read --after`
boundaries, ordinary append without hidden freshness, and one history visible
to all Room members.

The service has no browser console and no authentication. Bind to localhost or a trusted network only.

See [the HTTP API](../reference/http-api.md) for the target wire contract and
[status.md](../architecture/status.md) for the legacy differences.
