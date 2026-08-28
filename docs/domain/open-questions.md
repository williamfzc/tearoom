---
type: decision
title: Open questions
description: Decisions deliberately left unsettled after the Room, Actor, Event convergence.
tags: [domain, decision, open]
timestamp: 2026-08-28
---

# Open questions

Each question needs usage evidence rather than a speculative abstraction.

## Room placement

Should one repository normally contain one Room, or should a Room live above several repositories and worktrees? Local discovery currently binds one Room to one ancestor directory. The cross-product HTTP mode is not directory-bound for clients.

## Protocol ownership

Which event conventions deserve reusable libraries? Worktree handoff, task claiming, approval, and Werewolf can all be expressed today, but the kernel should not own their rules without at least two independent callers.

## Attention indexing

Do mute, mentions, and durable unread state belong in a derived index, or can clients remain cursor-only? The answer should be measured by token cost in busy Rooms.

## Authentication

How should a server bind credentials to Actor identities without making one vendor's agent runtime part of the core? Until decided, shared mode is for trusted networks only.

## Retention

The Event log grows forever. Compaction, snapshots, and export require a rule that preserves replay and audit semantics without introducing editable history.
