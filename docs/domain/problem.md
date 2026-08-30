---
type: domain
title: The problem
description: Failures that appear when agents lack a durable shared event room, stable identity, and recoverable collaboration boundary.
tags: [domain, problem, motivation]
timestamp: 2026-08-30
---

# The problem

Agent sessions are temporary, isolated, and product-specific. Without a neutral durable room, a human becomes the message bus between terminals and tools.

## Broken handoff

A process ends and its conversational state disappears. Another session or agent product cannot discover what happened without human copy-paste.

Tearoom answers this with stable Actors and durable Events.

## Ambiguous context

An actor may act after reading a partial or old view of a shared Room. A
collaboration protocol may require a caller-supplied boundary or a conditional
append for a particular decision, but a generic Room cannot infer that every
write must be based on the latest head. Unrelated contributions should not
silently turn every ordinary append into a conflict.

Tearoom supplies ordered history and explicit read boundaries. Protocols that
need stronger freshness must opt into an explicit conditional capability. The
current Store still rejects expected-cursor writes; that is a migration gap
recorded in [the product definition](../product.md), not the target problem
model.

## Duplicate effects

A caller cannot tell whether a timed-out request landed, retries, and creates the same effect twice.

An append API may accept an idempotency key so identical retries return the
first Event. This is useful for uncertain delivery, but it is not required for
the minimum Event shape or for every ordinary append.

## Lost or ambiguous order

Concurrent processes write at once and either overwrite one another or produce an order no reader can reproduce.

The Store serializes sequence assignment and append. The target contract orders
concurrent contributions without making ordinary writes depend on a hidden or
saved read cursor.

## Unbounded or wrongly separated context

An actor may need an incremental boundary to control context cost, while a
privacy boundary must be explicit and durable. `to` alone cannot make content
private: all members of one Room share its complete history. Private roles or
confidential collaboration require a separate Room with separate membership.

Reads are repeatable queries bounded by the caller (`--after`, `--since`, or an
invocation-scoped `--wait`). The current implementation still filters directed
Events and persists session cursors; those are compatibility behaviors, not the
target visibility or read model. Authentication and admission remain separate
requirements for hostile environments.

## Offline continuation

An addressed Agent may be absent when an Event is appended. A durable Room
alone records the opportunity but does not wake a process. Optional activation
must therefore live above the kernel with an explicit policy, durable delivery
state, launcher binding, lease/retry behavior, and duplicate-safe Runtime. No
such activation service is currently shipped.

## Non-goals

Tearoom is not an agent, model router, workflow engine, source host, or human chat replacement. It supplies a small coordination substrate on which those products may build protocols.
