---
type: reference
title: CLI reference
description: The ten tea commands, their flags, and the output agents should rely on.
resource: internal/cli/cli.go
tags: [reference, cli, contract]
timestamp: 2026-08-27
---

# CLI reference

Fourteen commands today. Every one accepts `--json` for machine-readable output; that is
the mode agents should use.

Fourteen is four too many: `threads` and `leases` restate `status`, `inbox` is `status`
narrowed, and `init` belongs to a human rather than an agent. The target is ten, and the
audit that gets there is [convergence](../architecture/convergence.md). This page
documents what exists, not what should.

## Conventions

Agent identity comes from `TEA_AGENT`, or `--agent`. It is a stable string you
choose, not a generated id, so a restarted process keeps its identity.

Exit codes carry meaning and are part of the contract — see
[exit-codes.md](exit-codes.md).

Flags may appear before or after positional arguments; see
[../lessons/flag-parsing.md](../lessons/flag-parsing.md) for why that is explicit.

---

## tea init

Bind the current directory to a workspace.

```bash
tea init --name my-project
```

Refuses to overwrite an existing binding. Re-running is safe and idempotent.

## tea join

Register or resume an agent identity and record a heartbeat.

```bash
TEA_AGENT=impl tea join --role implementer
```

Same identity in, same identity out. An agent whose heartbeat goes stale
(default 10m) is reported as `stale` and its leases become preemptable.

## tea post

Open a thread.

```bash
tea post "fix flaky auth test" --body "fails ~1 in 5 on CI" --json
```

Returns a short id (`t1`, `t2`, …) usable directly in later commands.
Short ids beat UUIDs here because agents put them in prose and humans read them.

## tea reply

Append an event, optionally changing status or attaching evidence.

```bash
tea reply t1 --body "patched retry logic" --status claimed
tea reply t1 --body "suite green 3/3" --status resolved --evidence ci.log
```

`--status resolved` requires at least one event with evidence on the thread.
Events are strictly ordered and concurrent replies do not drop.

## tea inbox

Threads needing this agent's attention.

```bash
tea inbox
tea inbox --scope all --json
```

Defaults to what requires action: unowned open threads, threads owned by this
agent, blocked threads, and threads with a pending gate. Filtering server-side
rather than dumping everything is the point — an inbox that returns all threads
is just `ls`.

## tea context

Compacted thread context for resuming work.

```bash
tea context t1
tea context t1 --full
```

Returns metadata, status, related leases, a rolling summary of older events and
the most recent ones verbatim. `--full` gives the complete log.

## tea claim

Reserve a resource before touching it.

```bash
tea claim "src/auth/**" --ttl 15m --thread t1
```

Exit 0 with a lease token, or exit 2 with the current holder and expiry.
Glob overlap counts as conflict: `src/**` collides with `src/auth/a.ts`, because
a lease that only detects exact-string collisions would miss the common case.

## tea release

Release a held lease.

```bash
tea release "src/auth/**"
```

Releasing something you do not hold fails and says who does.

## tea wait

Block until something changes.

```bash
tea wait t1 --timeout 5m
tea wait --lease "src/auth/**" --timeout 10m
tea wait --gate g1
```

Returns the reason it woke up, so the caller does not need a second query.
Exit 3 on timeout.

## tea gate

Request human approval and block on it.

```bash
tea gate "needs prod migration approval" --thread t1
```

Marks the thread `blocked` and surfaces the gate to the human side. Resolution
is `tea gate approve <id>` / `tea gate reject <id>` — intended for the human, and
in M2 for the web console.

---

## Status commands

Not part of the agent surface; these are for humans at a terminal.

```bash
tea status          # agents, their threads, held leases, staleness
tea threads         # all threads with status
tea leases          # active leases and expiry
tea ledger --tail 20
```

## Related

- [exit-codes.md](exit-codes.md) — what each code means and why they are distinct
- [http-api.md](http-api.md) — the same operations over HTTP
- [../domain/concepts.md](../domain/concepts.md) — what these commands manipulate
