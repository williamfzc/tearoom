---
type: runbook
title: Building and testing
description: Build the single Tearoom binary and run its verification suites.
resource: Makefile
tags: [runbook, build, testing]
timestamp: 2026-09-09
---

# Building and testing

Requires Go 1.22+ and a Unix-like OS.

```bash
make build          # bin/tearoom
make cli            # the shipped binary, built fresh
make test           # unit, HTTP, and independent-process concurrency tests
make test-race      # race detector
make target-room    # E1: the room-tree contract over the JSONL store and HTTP
make target-replay  # E2: replay across real processes, no read progress
make check-evals    # the evaluation set stays bidirectionally consistent
make docs           # the docs bundle stays consistent (frontmatter, links)
make check          # fmt vet test test-race target-room target-replay check-evals line-budget docs
```

The [evaluation set](../evaluations.md) is the authority for what each suite
proves; `check-evals` enforces that every shipped evaluation has its script
marker and its place in `make check`.

## Toolchain traps

**macOS test linking.** On macOS, tests use the system linker because the Go
1.22 internal linker can emit test executables without the `LC_UUID` required
by current macOS; test binaries whose closure includes the pure-Go SQLite
stack abort at launch without it. The Makefile encodes this in its Darwin
branch (`CGO_ENABLED=1`, `-ldflags=-linkmode=external`), so run tests with
`make test`/`make test-race` rather than bare `go test`. Release builds use
`CGO_ENABLED=0` to produce one static binary.

**Module proxy.** This machine cannot reach `proxy.golang.org` (dial
timeouts). The Makefile exports `GOPROXY ?= https://goproxy.cn,direct` so every
`make` target works out of the box; override it with
`make GOPROXY=https://proxy.golang.org` where the default mirror is not
desired. Standalone `go` commands need the same setting, e.g.
`GOPROXY=https://goproxy.cn,direct go build ./...`.
