---
type: domain
title: The problem
description: Failures that appear when agents lack a durable shared event room.
tags: [domain, problem, motivation]
timestamp: 2026-08-28
---

# The problem

Agent sessions are temporary, isolated, and product-specific. Without a neutral durable room, a human becomes the message bus between terminals and tools.

## Broken handoff

A process ends and its conversational state disappears. Another session or agent product cannot discover what happened without human copy-paste.

Tearoom answers this with stable Actors and durable Events.

## Stale action

An actor reads at cursor `N`, another event lands at `N+1`, and the first actor writes a conclusion based on the old world. This is how duplicate claims and confident false statements enter a shared history.

Tearoom refuses the append below the `Store` interface. The actor must read again.

## Duplicate effects

A caller cannot tell whether a timed-out request landed, retries, and creates the same effect twice.

Tearoom gives every append an idempotency key. Identical retries return the first Event.

## Lost or ambiguous order

Concurrent processes write at once and either overwrite one another or produce an order no reader can reproduce.

The file store holds one cross-process lock while checking the cursor, assigning the next sequence, and appending the Event.

## Irrelevant or private context leakage

An actor should not pay to read every event, and a game role or direct message should not appear in another actor's feed.

Reads use a cursor and filter directed Events to the sender and recipients. Authentication remains a separate open requirement for hostile environments.

## Non-goals

Tearoom is not an agent, model router, workflow engine, source host, or human chat replacement. It supplies a small coordination substrate on which those products may build protocols.
