---
type: architecture
title: Convergence
description: Why Tearoom exposes workspace, server, and Event concerns with a short common path.
resource: internal/cli/cli.go
tags: [architecture, cli, convergence, decision]
timestamp: 2026-08-28
---

# Convergence

Mirroring every domain entity into a CLI resource made internal structure more visible than user intent. The CLI now groups commands around three operational concerns:

```text
tearoom room init|info
tearoom server start
tearoom event send|list|watch
```

## Responsibilities

| Concern | Commands | Responsibility |
|---|---|---|
| `room` | `init`, `info` | Local workspace lifecycle |
| `server` | `start` | Independent service process |
| `event` | `send`, `list`, `watch` | Normal observation and action loop |

Actor remains a domain entity, but registration is automatic on the first Event operation. It does not deserve a separate user ritual.

## Common path and protocol path

The common path has no protocol bookkeeping:

```bash
tearoom event send "review worktree A"
tearoom event list
tearoom event watch
```

The CLI supplies the default Event type, saved cursor, and derived id. Less common protocol controls remain explicit flags: `--body`, `--data`, `--type`, `--to`, `--caused-by`, `--cursor`, and `--id`.

## Local state boundary

The saved Actor cursor is local client state, not Room truth. `list` and `watch` advance it after a successful read. `send` uses it as the expected Room version. It never refreshes the cursor before writing, because freshness must describe what the Actor actually observed.

The Store remains authoritative for Actor admission, stale-write refusal, idempotency, ordering, persistence, and visibility.

## Naming rules

- `room` and `server` are low-frequency management concerns.
- `event` is the only normal interaction namespace.
- `send` accepts positional text because text is its primary object.
- `list` means return currently available Events and exit.
- `watch` means wait when no later Event exists.
- No aliases preserve earlier command grammars.
