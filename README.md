# Tearoom

A durable shared Room where people, Agents, and tools contribute to one ordered,
replayable history. An optional activation layer may wake an absent Agent so it
can recover context from the Room and continue the collaboration.

[`docs/product.md`](docs/product.md) is the authoritative product definition.
[`docs/architecture/status.md`](docs/architecture/status.md) separates that
target from the currently shipped v0.1 behavior.

## Minimum model

- **Room** — the membership and history boundary.
- **Actor** — a stable participant identity, independent of a process or Agent
  session.
- **Event** — one immutable contribution ordered within its Room.

Every Room member sees the complete Room history. `to` is optional public
addressing, not secrecy and not a command to start a process. Privacy requires a
different Room with different membership.

The target common path is deliberately small:

```text
tearoom send <content>
tearoom read [--after <event>] [--since <time>] [--wait <duration>]
```

Reading is side-effect free. Tearoom does not save where an Actor has read, and
ordinary append does not depend on hidden read progress. Callers that need an
incremental boundary pass one explicitly.

## Optional Agent activation

A Room works without an Agent runtime or activation service. When activation is
enabled, an operator-owned policy may elect an addressed Event, start or resume
the target Actor's runtime, and pass a Room and Event reference. The Agent then
reads the Room and appends its contribution.

Activation delivery state belongs to that optional control plane. It is not
Actor unread state, and a successful process launch is not proof that the
collaboration task succeeded.

## Current implementation

The repository still implements the previous v0.1 contract. Its executable CLI
uses `event send`, `event list`, and `event watch`; it persists per-Actor client
cursors, requires an expected cursor on append, filters directed Events, and
stores `type` and `caused_by`. Those behaviors are migration debt, not the target
product contract.

Use the current binary only with that distinction in mind:

```bash
tearoom room init --name project

export TEAROOM_ACTOR=alice
tearoom event send "please review worktree A"
tearoom event list
tearoom event watch
```

The hosted server and management UI also run the v0.1 contract. See the
[migration status](docs/architecture/status.md), [target CLI](docs/reference/cli.md),
and [evaluation set](docs/evaluations.md).

## Build and verify

Requires Go 1.22+ and a Unix-like OS.

```bash
make build
make test
make smoke
make session-smoke
make check
```

The current suites verify the v0.1 implementation. They do not yet prove the
new public-visibility, pure-read, unconditional-append, or offline-activation
stories.

## License

MIT
