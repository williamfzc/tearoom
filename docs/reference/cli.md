---
type: reference
title: CLI reference
description: Tearoom workspace, server, and Event commands.
resource: internal/cli/cli.go
tags: [reference, cli, contract]
timestamp: 2026-08-28
---

# CLI reference

```text
tearoom <command> [subcommand] [flags]
```

The top-level concerns are `room`, `server`, and `event`. Room manages local workspace state, Server runs the service process, and Event is the normal interaction path.

## Conventions

- JSON is the default output; `--format text` selects compact human output.
- Actor identity comes from `TEAROOM_ACTOR` or `--as`.
- The first Event operation registers the Actor automatically.
- `TEAROOM_SERVER` switches from the nearest local `.tearoom` directory to a shared server.
- A local session file stores one cursor per Room and Actor.
- Every command level supports `--help`.

## Room

### `tearoom room init`

```bash
tearoom room init --name project
```

Creates `.tearoom` in the current directory and refuses to overwrite an existing Room.

### `tearoom room info`

```bash
tearoom room info
```

Shows the current Room.

## Server

### `tearoom server start`

```bash
tearoom server start --address :8787 --directory /path/to/project
```

Exposes one local Room over HTTP. No browser application is served.

## Event

### `tearoom event send`

```bash
tearoom event send "review worktree A"
```

The common path sends a public `message` Event. The CLI uses the Actor's saved cursor as the expected Room version and derives an idempotency key from Actor, cursor, type, content, recipients, and cause.

A successful send advances the saved cursor to the appended Event. A stale write exits 2 and does not advance local state.

Optional protocol controls:

- `--as`: Actor identity; defaults to `TEAROOM_ACTOR`.
- `--body`: text body instead of positional text.
- `--data`: JSON data instead of text.
- `--type`: Event type; defaults to `message`.
- `--to`: comma-separated recipients.
- `--caused-by`: causing Event id.
- `--cursor`: override the saved expected cursor.
- `--id`: override the derived idempotency key.

The CLI never reads the latest cursor immediately before sending. That would turn an obsolete assumption into an apparently fresh write.

### `tearoom event list`

```bash
tearoom event list
```

Returns Events currently available after the Actor's saved cursor and exits immediately. A successful list advances the cursor, including past directed Events hidden from that Actor.

Optional controls:

- `--as`: Actor identity.
- `--cursor`: override the saved starting cursor.
- `--limit`: maximum visible Events; zero means all.

### `tearoom event watch`

```bash
tearoom event watch --timeout 5m
```

Starts after the saved cursor. If the Room has already advanced, it returns immediately; otherwise it waits. A successful watch returns the same Feed shape as `event list` and advances the cursor. Timeout exits 3 without changing local state.

Against a server the wait is held by the server (long-poll, sliced into requests of at most 30 seconds), so a quiet Room costs the waiting client and the server nothing until an Event appears. Against a local Room the CLI polls at `--poll-interval`.

Optional controls:

- `--as`: Actor identity.
- `--cursor`: override the saved starting cursor.
- `--timeout`: maximum wait.
- `--poll-interval`: local polling interval; ignored when `TEAROOM_SERVER` is set.

## Actor bootstrap and directed Events

An Event operation registers its own Actor automatically. A directed Event still requires every recipient to exist in the Room. The current short CLI has no member-management command, so a recipient must previously have performed an Event operation or been admitted through the HTTP API. This keeps recipient validation in Store without adding another top-level command prematurely.

## Session state

Local session state lives in the user configuration directory, outside the Room workspace. `TEAROOM_HOME` overrides that location for isolated clients and tests. Each filename is derived from the Room scope and Actor identity; the file contains the Actor id and last observed cursor. Session state is local convenience, not shared truth. The Store still validates every Event append.

When `TEAROOM_SERVER` is set, the normalized server URL scopes session state, so clients do not need a local `.tearoom` directory. Reusing the same Actor and server URL resumes the same cursor across processes.
