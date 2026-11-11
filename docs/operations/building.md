---
type: runbook
title: Building and testing
description: Build the single Tearoom binary and run its verification suites.
resource: Makefile
tags: [runbook, build, testing]
timestamp: 2026-09-09
---

# Building and testing

Requires Rust with Cargo, Clippy and rustfmt, and a Unix-like OS. The release
workflow pins the Rust toolchain version; build with the same version when
reproducing release artifacts.

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

Go compatibility and real hc integration tests are marked ignored because
they require external binaries. Run them explicitly with the corresponding
test environment when validating those boundaries.
