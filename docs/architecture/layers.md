---
type: architecture
title: Layers and dependency direction
description: The four layers of Tearoom, the responsibility each owns, and the rule that dependencies only ever point downward.
resource: internal/
tags: [architecture, boundaries, layering]
timestamp: 2026-08-27
---

# Layers and dependency direction

Four layers. Each supports the one above it and knows nothing about it.

## The layers

| Layer | Package | Owns | Must not |
|---|---|---|---|
| Foundation | `internal/core` | The five concepts, the `Store` contract, typed errors | Import anything from this project |
| Domain | `internal/store`, `internal/view` | Persistence and locking; pure derivation of state | Know about HTTP, terminals, or flags |
| Application | `internal/resolve` | Assembling raw reads into derived state | Hold product opinions or format output |
| Outside | `internal/cli`, `internal/transport`, `cmd/` | Users, networks, files, processes, protocols | Contain rules that other clients also need |

## The rule

Dependencies point down. `view` may use `core`; `core` may never use `view`. Peers do
not reach sideways: `cli` and `transport` are both clients of the same lower layers
and must not import each other's internals.

When two peers need the same behaviour, the answer is never to copy it. It moves down
into a layer with a clear owner. Two implementations of one rule are not two
opinions — they are one bug waiting for the second caller to expose it.

## Why `resolve` exists

Every client needs the same five reads — workspace, agents, threads, leases, gates —
in the same order before it can derive anything. `view` cannot do those reads, because
it is pure by design: no I/O, no clock. So the sequence was written in the CLI, and
then written again in the HTTP server.

That is one fact stated twice. `internal/resolve` now owns it, and both clients call
it. It holds no product opinion of its own: judgement stays in `view`, persistence in
`store`, presentation in the layer above.

The clock is passed in as a `Clock` function rather than read from `time.Now`. That is
what keeps the layer testable and lets the server and CLI share one implementation.

## Verifying the rule

The import graph is small enough to check directly:

```bash
for p in core store view resolve transport cli; do
  echo "-- $p:"
  grep -h "williamfzc/tearoom" internal/$p/*.go | grep -v _test |
    sed 's/.*tearoom\///;s/"//' | sort -u | sed 's/^/     /'
done
```

`core` should list nothing. `store` and `view` should list only `core`. If a lower
layer ever names a higher one, the violation is real regardless of how convenient it
looked.

## Related

- [isomorphic-go.md](isomorphic-go.md) — why one language makes this layering pay off
- [../lessons/invariants-belong-in-the-store.md](../lessons/invariants-belong-in-the-store.md) — the bug that established where rules live
- [../domain/concepts.md](../domain/concepts.md) — the vocabulary the domain layer protects
