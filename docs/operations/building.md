---
type: runbook
title: Building and testing
description: Build the single Tearoom binary and run its verification suites.
resource: Makefile
tags: [runbook, build, testing]
timestamp: 2026-08-30
---

# Building and testing

Requires Go 1.22+ and a Unix-like OS.

```bash
make build          # bin/tearoom
make test           # unit, HTTP, and independent-process concurrency tests
make test-race      # race detector
make smoke          # file-backend smoke: real binary and exit-code contract
make smoke-sqlite   # the same contract over the SQLite backend
make session-smoke  # same-product and cross-product session exchange
make harness-demo   # the harness workflow story end to end
make game-demo      # one Werewolf round over directed role Events
make worktree-demo  # two real git worktrees sharing one Room
make hosted-demo    # the hosted credential matrix: auth, binding, revocation
make pg-smoke       # the same invariants over PostgreSQL (skips without TEAROOM_TEST_PG)
make check-evals    # the evaluation set stays bidirectionally consistent
make check          # all checks plus documentation validation
```

These commands exercise the currently shipped v0.1 implementation. In
particular, the smoke, session, harness, game, and worktree suites still encode
saved cursors, mandatory stale-write rejection, typed or causal Events, and
directed visibility. They are regression evidence for the code being migrated,
not proof of the adopted target contract.

Release builds use `CGO_ENABLED=0` to produce one static binary. On macOS, tests use the system linker because the Go 1.22 internal linker can produce test executables without the `LC_UUID` required by current macOS.

## What the suites prove

The [evaluation set](../evaluations.md) is the authority for what each suite
actually proves. `check-evals` verifies that its story-to-evaluation map remains
structurally consistent; it cannot by itself prove that a legacy suite covers a
new product story.

PostgreSQL validation is gated: `make pg-smoke` and the `TEAROOM_TEST_PG` unit battery skip with a notice when the environment variable (a `postgres://` DSN) is absent, and run the full suite wherever a database exists.
