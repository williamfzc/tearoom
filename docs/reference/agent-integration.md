---
type: reference
title: Agent integration guide
description: A minimal, replayable integration path for an Agent working inside a Tearoom room tree.
tags: [reference, agents, integration, cli, http]
timestamp: 2026-11-07
---

# Agent integration guide

This guide connects an Agent to the contracts in [cli.md](cli.md) and
[http-api.md](http-api.md). It does not define a task or workflow schema; what
a record means is a convention the workers write. The binary itself carries an
orientation guide: run `tearoom skill` before any setup.

## Find the workspace

Run commands from the project directory or one of its descendants. Tearoom
discovers the nearest ancestor `.tearoom` workspace; it does not create Rooms
during `send`, `read`, or `watch`. Create the first Room once:

```bash
tearoom room init --name main-line
```

That first command also creates the workspace. Add children from the Room
that owns the goal:

```bash
tearoom room init --name implementation --parent main-line
tearoom room init --name review --parent main-line
```

Room names are the addresses everyone else will read; quote a record as
`room-name#seq` in plain text.

## Write and continue

`send` appends one record to the target Room. `--as` sets the signature — a
plain label, never verified; omit it and the record carries an empty author.

```bash
export TEAROOM_ACTOR=codex          # a stable signature, chosen once
tearoom send --text "goal recorded" --room main-line
tearoom send --json '{"done":"checkout flow","blocked":"none","next":"reviewer reads implementation"}' --room implementation
```

Plain text is encoded as a JSON string; `--json` passes any valid JSON value.
`send` prints one JSON Record object; parse `content` as JSON.

## Read what you need

```bash
tearoom read --room implementation                  # the whole stream
tearoom read --room implementation --tail 5         # where this branch stands
tearoom read --room implementation --subtree        # the room and its descendants
tearoom read --room implementation --after e42      # from your own boundary
tearoom tree                                        # the skeleton: parents and children
```

Reads are pure and repeatable. Keep your own boundary in `--after`; Tearoom
stores no cursor. An empty read is success: no output, exit 0.

## Wait for new work

```bash
tearoom watch --json
```

One JSON envelope per new record, until interrupted:

```json
{"room":"implementation","seq":7,"author":"codex","at":"2026-11-07T10:02:00Z"}
```

The envelope says only "there is something new". Deciding who wakes and how
belongs to an external waker — for example a script that watches this output
and drives `herdr agent prompt <name> "<read the room and continue>"`.
Tearoom never starts a process itself.

## Work with another process over HTTP

```bash
tearoom server start --address 127.0.0.1:8787
export TEAROOM_SERVER=http://127.0.0.1:8787
```

The server binds loopback only and serves one workspace; room selection rules
are unchanged. `POST /api/events` carries `room` and `content`;
`X-Tearoom-Author` carries the signature; errors are structured as
`{"error":{"code":"...","message":"..."}}`. The copyable request sequence is
in the [HTTP curl journey](http-api.md#minimal-curl-journey).

## Idempotent retries

If a `send` may have gone through, retry with the same `--idempotency-key`
and you get the original Record back instead of a duplicate. The key is
scoped per (room, signature, key); reusing it with different content is a
conflict and exits 1.

## The three journeys this must hold for

1. One Agent completes a task alone, reading only its Room.
2. A reviewer Agent reads only the implementation Room and returns pass or
   fail with evidence; the executor never certifies its own work.
3. A new process, a day later, reads only the Rooms and answers: goal,
   previous work, acceptance, next step.

The executable target and the hand-run method live in
[evaluations.md](../evaluations.md).
