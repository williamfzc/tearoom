---
type: lesson
title: Accept arguments in the order callers actually write them
description: Go's flag package stops at the first positional argument, so `tea post "title" --json` silently swallowed the flag into the title.
resource: internal/cli/cli.go
tags: [lesson, cli, ergonomics]
timestamp: 2026-08-27
---

# Accept arguments in the order callers actually write them

## What happened

Go's `flag` package stops parsing at the first non-flag argument. So:

```bash
tea post "fix the flaky test" --json
```

parsed `--json` as part of the title and returned human-readable text. An agent asking
for JSON would receive prose and fail to parse it — with no error to explain why.

## Why it matters more here than usual

The primary caller is a program. A human notices missing JSON immediately; an agent
gets a parse failure several steps removed from the cause, and the real mistake is
invisible at the point it is discovered.

Flags are also written in whatever order reads naturally. Insisting they precede
positionals is a rule the caller has no reason to expect.

## The fix

Reorder arguments before parsing: flags first, positionals after, with `--` respected
so a title containing `--json` survives verbatim.

Handled explicitly: bool flags take no value, so the token after one is a positional,
not its argument. Getting that wrong would silently drop the title.

## Guarded by

Eight cases in `internal/cli/cli_test.go`, including the `--` escape, the `--flag=value`
form, the bool-then-positional case, and a typo that must be rejected rather than
absorbed into the title.

## Related

- [../reference/cli.md](../reference/cli.md) — the command surface
- [../reference/exit-codes.md](../reference/exit-codes.md) — the other half of the machine contract
