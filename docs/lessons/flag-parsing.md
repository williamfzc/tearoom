---
type: lesson
title: Accept arguments in the order callers write them
description: Flag placement must never decide meaning; Tearoom parses flags and positional text in any order.
resource: src/cli.rs
tags: [lesson, cli, ergonomics]
timestamp: 2026-08-30
---

# Accept arguments in the order callers write them

Callers write flags after the positional text:

```bash
tearoom send "review complete" --to reviewer
```

The Go implementation had to defend this: its `flag` package stops at the
first positional argument, so the CLI reordered known flags before parsing.
The Rust parser needs no wrapper — it walks the arguments once and buckets
flags and positional text wherever they appear. In both designs `--` remains
the literal boundary, boolean flags do not consume the next token, value
flags do, and unknown flags fail loudly.

The primary caller is often a program. Silently treating `--to` or another
recognized option as content would change the record rather than merely its
presentation. The parser lesson survived both the command migration and the
language cutover: the shipped CLI exposes the room-tree commands (`room`,
`send`, `read`, `tree`, `watch`, `server`, `skill`, `role`, `collab`), with
no legacy `event` group or flags.

Guarded by `tests/cli_room.rs`.
