---
type: runbook
title: Building and testing
description: Make targets, the four test suites, and the two toolchain traps that produce confusing failures.
resource: Makefile
tags: [runbook, build, testing, cgo, wasm]
timestamp: 2026-08-27
---

# Building and testing

Requires Go 1.22+ and a Unix-like OS. `flock` is used for store mutations; see the
Windows gap in [../lessons/cross-process-locking.md](../lessons/cross-process-locking.md).

```bash
make build          # bin/tea, bin/tea-server, web/tea.wasm
make test           # unit tests, including multi-process concurrency
make test-wasm      # frontend tests, run in node
make test-race      # everything under the race detector
make smoke          # 41 assertions against the real binary
make check          # all of the above; run before pushing
```

## Trap 1: build with make, not go build

The Makefile sets `CGO_ENABLED=0`. This is not a preference:

- it produces a static binary with no linker variation between machines, which is the
  right default for a tool people install by dropping it on `PATH`
- with cgo enabled, `net/http` on this macOS toolchain produces a binary that dies at
  startup with `dyld: missing LC_UUID load command`

A bare `go test ./...` hits the same failure in the transport package. If a binary
aborts immediately with a dyld error, this is why.

## Trap 2: wasm tests need a trimmed environment

`GOOS=js` tests run in node through Go's exec wrapper, which passes the environment
through the wasm command line. A normal shell environment overflows that limit:

```
Error: total length of command line and environment variables exceeds limit
```

`make test-wasm` handles this with `env -i` and a minimal `PATH`. Note that node's own
path may contain spaces, so it must stay quoted — an unquoted `PATH` silently produces
`node: not found`.

If node is absent the target skips rather than fails, so the suite still runs on a
machine without it.

## What each suite is for

| Suite | Catches |
|---|---|
| `test` | Logic, and cross-process lease exclusion using real processes |
| `test-wasm` | Console repaint suppression — see [../lessons/console-repaint.md](../lessons/console-repaint.md) |
| `test-race` | Data races in the server's concurrent handlers |
| `smoke` | Contract behaviour of the shipped binary: exit codes, JSON shape, gate transitions |

`smoke` takes around 25 seconds because it exercises real `wait` timeouts. That is not
a hang.

## Related

- [serving.md](serving.md) — running what you built
- [../architecture/isomorphic-go.md](../architecture/isomorphic-go.md) — why there is a wasm target at all
