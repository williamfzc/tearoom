---
type: reference
title: CLI reference
description: Target Room commands and the current implementation boundary.
resource: internal/cli/cli.go
tags: [reference, cli, contract]
timestamp: 2026-08-30
---

# CLI reference

This document separates the target command shape from the commands currently
shipped by `internal/cli/cli.go`. The target follows the model rationale:
`read` is the normal observation command, waiting is a read mode, and no
command depends on a server-owned Actor cursor.

## Target command contract

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

## Current implementation boundary

The shipped binary still exposes `event send`, `event list`, and `event watch`, auto-registers Actors, persists local per-Actor cursors, filters directed Events, and uses cursor-bound append with `type`, `caused_by`, and idempotency flags. Those commands are legacy implementation behavior and are not the target contract above.

Current command names and flags remain available for compatibility:

- `tearoom event send "..."` currently sends a public `message` Event.
- `tearoom event list` currently starts after and advances a saved local cursor.
- `tearoom event watch` currently waits after and advances that saved cursor.
- `--body`, `--data`, `--type`, `--to`, `--caused-by`, `--cursor`, and `--id` are current compatibility flags, not target requirements.

The current behavior is documented only so an operator can identify the implementation being migrated. It must not be used as evidence that the target model is shipped.

## Activation boundary

The CLI does not currently wake offline Actors. A future activation service may observe an Event, deliver its reference to a launcher, and let a Runtime invoke `read` and `send`. That service owns delivery attempts and recovery; neither `read` nor `send` should acquire a hidden read cursor or scheduler responsibility.
