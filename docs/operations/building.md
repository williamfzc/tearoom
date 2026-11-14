---
type: runbook
title: Building and testing
description: Build the single Tearoom binary and run its verification suites.
resource: Makefile
tags: [runbook, build, testing]
timestamp: 2026-11-14
---

# Building and testing

Requires Rust with Cargo, Clippy and rustfmt, Zig 0.16, and a Unix-like OS.
Zig is required for any cargo build: the Ghostty terminal component vendored
inside the herdr-core dependency compiles Zig source in its cargo build
script, so the requirement is invisible to a repository-only search. The
Linux release targets additionally need the musl cross toolchain (the
release workflow installs `musl-tools`).
Cargo fetches herdr-core and its pinned Ghostty terminal component from Git
when needed. The release workflow pins the Rust toolchain version; build with
the same version when reproducing release artifacts.

```bash
make build          # bin/tearoom
make cli            # the shipped binary, built fresh
make test           # unit and independent-process concurrency tests
make target-room    # E1: the room-tree contract through the shipped binary
make target-replay  # E2: replay across real processes, no read progress
make check-evals    # the evaluation set stays bidirectionally consistent
make docs           # the docs bundle stays consistent (frontmatter, links)
make check          # formatting, Clippy, tests, E1/E2, docs, line budget and release checks
```

The [evaluation set](../evaluations.md) is the authority for what each suite
proves; `check-evals` enforces that every shipped evaluation has its script
marker and its place in `make check`.

The [Test workflow](../../.github/workflows/test.yml) runs `make check` for
branch pushes and pull requests on Ubuntu using the release toolchain.

Go compatibility tests are marked ignored because they require external
binaries. The hc runtime is built into Tearoom; real-Agent integration journeys
still require an installed and authenticated Agent CLI. Run ignored integration
tests explicitly when validating those boundaries.
