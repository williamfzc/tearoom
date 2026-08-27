---
type: log
title: Change log
description: Dated record of decisions and structural changes, newest first.
tags: [log, history]
timestamp: 2026-08-27
---

# Change log

Newest first. Records decisions and structural moves, not routine commits.

## 2026-08-27 — Adopt AGENTS.md, extract the assembly layer

Adopted `AGENTS.md` as the working standard for this repository, then audited the
existing code against it. Three violations found and fixed:

- **One fact stated twice.** The five-read sequence that assembles a board existed in
  both `internal/cli` and `internal/transport`, byte-for-byte. Extracted to
  `internal/resolve`, which now owns assembly and nothing else. The same duplication in
  thread context (live-lease and pending-gate filtering) went with it.
- **A file without a header.** `internal/transport/server.go` had no opening comment
  stating what it is or where it stands.
- **An unnamed constant.** The compact-event count was an inline literal in two places.

Added nine contract tests for `internal/resolve` covering the promises it makes to the
layer above: expired leases and settled gates never pass through, and a partial read
fails rather than producing a half-true board.

Rebuilt `docs/` as an OKF bundle: frontmatter on every document, one concept per file,
per-directory indexes, and links to canonical sources instead of restatement. The
previous `design.md` and `cli.md` were split by concern into `architecture/`, `domain/`,
`reference/`, `operations/` and `lessons/`.

## 2026-08-27 — M0 complete

CLI, HTTP server and WebAssembly console working end to end. Four lessons recorded
under [lessons/](lessons/index.md), each found by using the tool rather than reading it.
