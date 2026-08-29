---
type: runbook
title: Building and testing
description: Build the single Tearoom binary and run its verification suites.
resource: Makefile
tags: [runbook, build, testing]
timestamp: 2026-08-28
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
make check-evals    # the evaluation set stays bidirectionally consistent
make check          # all checks plus documentation validation
```

Release builds use `CGO_ENABLED=0` to produce one static binary. On macOS, tests use the system linker because the Go 1.22 internal linker can produce test executables without the `LC_UUID` required by current macOS.

## What the suites prove

The [evaluation set](../evaluations.md) is the map: every suite there is bound to the user story it proves (story ↔ evaluation), and `check-evals` fails if the map drifts — a suite removed from `make check` while its story still claims proof, or a marker dropped from a script. Nothing is re-listed here so the two cannot disagree.
