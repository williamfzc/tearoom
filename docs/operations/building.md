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
make target-room    # E1: the P0 Room contract over file, SQLite, and HTTP
make target-replay  # E2: P0 replay across real processes, no read progress
make check-evals    # the evaluation set stays bidirectionally consistent
make docs           # the docs bundle stays consistent (frontmatter, links)
make check          # fmt vet test test-race target-room target-replay check-evals docs
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

## PostgreSQL validation

The relational Store battery runs against PostgreSQL only when
`TEAROOM_TEST_PG` (a `postgres://` DSN) is set:

```bash
TEAROOM_TEST_PG=postgres://... go test -ldflags=-linkmode=external -count=1 ./internal/relational
```

Without the DSN the battery records an explicit skip; SQLite always runs the
same conformance battery. PostgreSQL validation is required before claiming the
shared service is ready for the remote deployment profile.
