---
type: reference
title: CLI reference
description: Authoritative target command contract for Room interaction.
resource: internal/cli/cli.go
tags: [reference, cli, contract]
timestamp: 2026-08-30
---

# CLI reference

This is the only CLI contract. `read` is the normal observation command,
waiting is a read mode, and no command depends on a server-owned Actor cursor.
Implementation status belongs in [status.md](../architecture/status.md).

## Command contract

```text
tearoom <command> [flags]

tearoom room init
tearoom room info
tearoom server start
tearoom send "review worktree A"
tearoom read [--after 12] [--wait 5m]
```

`room` manages local workspace state. `server` serves a shared Room. `send` appends one Event. `read` returns Events after the caller-supplied `--after` boundary and may wait for later Room movement when `--wait` is present. A caller may keep its own boundary in a session, but Tearoom does not save or advance one in shared Room state.

The target Event surface is deliberately small: ordered position, author, time, optional public `to` metadata, and content. The CLI must not require `type`, `caused_by`, a directed-private mode, or a mandatory expected cursor for ordinary append.

Target conventions:

- JSON is the default output; `--format text` selects compact human output.
- Actor identity comes from `TEAROOM_ACTOR` or `--as`.
- A caller authenticates as an Actor when the hosted service requires it.
- `--after` is an explicit observation boundary owned by the caller.
- `--wait` is invocation-scoped and does not acknowledge or mark Events read.
- A `to` value is visible addressing metadata; it does not hide the Event from Room members or request process execution.

## Activation boundary

The CLI does not currently wake offline Actors. A future activation service may observe an Event, deliver its reference to a launcher, and let a Runtime invoke `read` and `send`. That service owns delivery attempts and recovery; neither `read` nor `send` should acquire a hidden read cursor or scheduler responsibility.
