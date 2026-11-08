---
type: runbook
title: Serving a workspace
description: Operate the local loopback server and the external wake loop.
resource: rust/src/cli/observation.rs
tags: [runbook, server, waker]
timestamp: 2026-11-07
---

# Serving a workspace

One binary, one deployment profile: the local loopback server over the
nearest workspace. There is no hosted service; exposing the server beyond one
trusted machine is a misdeployment.

## Local profile

Create a workspace, then expose it from the same binary on the loopback
interface:

```bash
cd /path/to/project
tearoom room init --name main-line
tearoom server start --address 127.0.0.1:8787
```

The local profile binds loopback only: it has no credentials, and everything
on the loopback interface belongs to one OS user — the same trust domain as
the `.tearoom` directory itself. Never bind it to a public interface.

Point another session or agent product at it:

```bash
export TEAROOM_SERVER=http://127.0.0.1:8787
export TEAROOM_ACTOR=codex
tearoom send --text "hello from another session"
tearoom read --tail 5
```

## The wake loop

Tearoom only says "there is something new"; waking is external. The shipped
pieces:

1. `tearoom watch --json` prints one notification envelope per new record
   (`{"room":...,"seq":...,"author":...,"at":...}`) and runs until
   interrupted.
2. An external waker script reads that output, follows the handoff records
   in the named room (who is next, what to do first), and calls
   `herdr agent prompt <name> "<read the room and continue>" --wait`.

Operational preconditions for unattended waking (measured 2026-11-07, see
[../evaluations.md](../evaluations.md)):

- Each Agent tool's one-time dialogs — trust-this-directory, hook review,
  version updates — must be pre-cleared on the host, or the first wake stalls
  behind a modal.
- Each Agent CLI must already be authenticated; the local `claude` binary
  authenticates through the LLMBox wrapper (`claude-w`), not a bare login.
- herdr does not watch files itself; the polling step belongs to the waker
  script.

Rollback uses the previous release with its data snapshot; the workspace
marker carries the format version, and incompatible layouts are refused,
not upgraded. See [the HTTP API](../reference/http-api.md) for the wire
contract and [status.md](../architecture/status.md) for implementation state.
