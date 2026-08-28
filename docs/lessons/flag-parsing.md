---
type: lesson
title: Accept arguments in the order callers write them
description: Go flag parsing stops at the first positional, so Tearoom normalizes arguments before parsing.
resource: internal/cli/cli.go
tags: [lesson, cli, ergonomics]
timestamp: 2026-08-28
---

# Accept arguments in the order callers write them

Go's `flag` package stops at the first positional argument. Without a wrapper, this command:

```bash
tearoom event send --body "review complete" --type review.completed --to reviewer
```

would treat the trailing flags as positional text.

Tearoom reorders known flags before parsing while preserving `--` as the literal boundary. Boolean flags do not consume the next token, value flags do, and unknown flags fail loudly.

The primary caller is a program. Silently treating `--body`, `--type`, or `--to` as message text would change the Event protocol, not merely its presentation.

Guarded by `internal/cli/cli_test.go`.
