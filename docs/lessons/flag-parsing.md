---
type: lesson
title: Accept arguments in the order callers write them
description: Go flag parsing stops at the first positional, so Tearoom normalizes arguments before parsing.
resource: internal/cli/cli.go
tags: [lesson, cli, ergonomics]
timestamp: 2026-08-30
---

# Accept arguments in the order callers write them

Go's `flag` package stops at the first positional argument. Without a wrapper, this command:

```bash
tearoom send "review complete" --to reviewer
```

would treat the trailing flags as positional text.

Tearoom reorders known flags before parsing while preserving `--` as the literal boundary. Boolean flags do not consume the next token, value flags do, and unknown flags fail loudly.

The primary caller is often a program. Silently treating `--to` or another
recognized option as content would change the Event rather than merely its
presentation. This parser lesson survives the command migration even though
the current implementation still exposes `event send` and additional legacy
flags.

Guarded by `internal/cli/cli_test.go`.
