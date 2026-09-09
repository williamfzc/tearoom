---
type: reference
title: Agent integration guide
description: A minimal, replayable integration path for an Agent using Tearoom's local or HTTP Room contract.
tags: [reference, agents, integration, cli, http]
timestamp: 2026-09-10
---

# Agent integration guide

This guide connects an Agent Runtime to the target contracts in
[cli.md](cli.md), [http-api.md](http-api.md), and
[storage-layout.md](storage-layout.md). It does not define a task, result, or
workflow schema. Application-level examples belong in
[harness-workflow.md](../domain/harness-workflow.md). The binary itself
carries an orientation guide: run `tearoom skill` before any setup.

## Find the Room

In the currently shipped local profile, run commands from the project directory or one of its
descendants. Tearoom discovers the nearest ancestor `.tearoom` directory; it
does not create a Room during `send`, `read`, or `room join`. Create one once:

```bash
tearoom room init --as planner
```

When using the same service over HTTP, set `TEAROOM_SERVER` to the loopback server URL. Client
commands then use that Room instead of local directory discovery. `room init`
is local in the current implementation; the server itself must be started from
a directory containing the Room. Remote Room creation will extend this same
service and CLI rather than define another product:

```bash
tearoom server start --address 127.0.0.1:8787
export TEAROOM_SERVER=http://127.0.0.1:8787
```

## Establish a stable Actor

Choose an explicit stable Actor identity that survives Runtime and process
restarts. Set `TEAROOM_ACTOR` for normal `send` and `read` calls, or pass
`--as <actor>` when a command needs a one-off identity. Admission is explicit
and positional:

```bash
export TEAROOM_ACTOR=worker
tearoom room join worker
```

Joining an existing Actor is repeatable and returns the existing membership.
An unjoined Actor cannot read or append.

## Replay

Keep the last boundary in the Agent's own state, not in Tearoom. After a
successful read, choose the largest returned `seq` and use its presentation
address on the next call:

```bash
tearoom read --after e42
```

The Room never advances a hidden Actor cursor. A Runtime that wants to wait
for later history polls this read from its own boundary; a wait is never a
subscription and is not proof of offline activation.

## Append safely and parse output

Plain-text `send` encodes its argument as a JSON string. Use `--json` for any
other valid JSON value. `send` prints one JSON Event object; `read` prints one
JSON Event per line (JSONL). Parse `content` as JSON rather than treating it as
an untyped text field.

For an uncertain append, supply an idempotency key and retry the same request
with the same key. An identical retry returns the original Event; reusing the
key with different content or addressing is a conflict and exits with code 1.

```bash
tearoom send --json '{"kind":"proposal"}' --idempotency-key proposal-42
```

## Use HTTP from a separate process/runtime

For the currently shipped loopback profile, the server and Runtime run on the same host. Send
`X-Tearoom-Actor` on every events request. `POST /api/events` accepts only
`to` and `content`; `Idempotency-Key` is a request header. Errors are structured
as `{"error":{"code":"...","message":"..."}}`. Reads are pure and unbounded: a
caller that polls for later history loops over the same `after` boundary and
treats an empty array as "nothing new".

The target remote profile keeps this Room/Event contract and command behavior,
while deployment supplies PostgreSQL, HTTPS, and credentials. Its current gap
is tracked in [status.md](../architecture/status.md).

The copyable request sequence is in the [HTTP curl journey](http-api.md#minimal-curl-journey).

## Complete continuation loop

The product story is complete only when one Agent product appends, a later
process in another product pulls that Event with `read`, writes its result with
`send`, and the first or a third product pulls the new result. A human follows
the same Actor path. The executable target and its four-step CLI sequence live
in [evaluations.md](../evaluations.md#p1--one-cross-product-continuation-story).
