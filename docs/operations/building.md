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
make smoke          # real binary and exit-code contract
make session-smoke  # same-product and cross-product session exchange
make check          # all checks plus documentation validation
```

Release builds use `CGO_ENABLED=0` to produce one static binary. On macOS, tests use the system linker because the Go 1.22 internal linker can produce test executables without the `LC_UUID` required by current macOS.

## What the suites prove

| Suite | Boundary |
|---|---|
| `test` | stale-write refusal, idempotency, private visibility, HTTP equivalence, and cross-process ordering |
| `test-race` | in-process race detection around concurrent Store use |
| `smoke` | shipped CLI, all exit classes, waiting, directed Events, and retries |
| `session-smoke` | later sessions and a raw HTTP client sharing one Room |
