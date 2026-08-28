---
type: index
title: Reference
description: Public contracts — the CLI surface, exit codes, and the HTTP API.
tags: [index, reference, contract]
timestamp: 2026-08-27
---

# Reference

The parts other programs depend on. Changing anything here is a breaking change.

| Document | Contract |
|---|---|
| [cli.md](cli.md) | The fourteen commands and their flags |
| [exit-codes.md](exit-codes.md) | What each exit status means |
| [http-api.md](http-api.md) | Endpoints exposed by `tea-server` |
| [storage-layout.md](storage-layout.md) | On-disk shape of a workspace |

## Who consumes these

Agents consume the CLI and its exit codes. The console consumes the HTTP API. Humans
consume the CLI's text output, which is the only surface here allowed to change freely —
`--json` is the stable one.
