# Tearoom

A durable shared Room where people, Agents, and tools contribute to one ordered,
replayable history. An optional activation layer may wake an absent Agent so it
can recover context from the Room and continue the collaboration.

[`docs/product.md`](docs/product.md) is the authoritative product definition.
Start with the [documentation route](docs/index.md). Current implementation
gaps live only in [status.md](docs/architecture/status.md); they are not an
alternate product contract.

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

## Implementation status

The executable has not yet reached this contract. Do not use the old command or
Event model as design guidance. The migration is a clean break: old commands,
schemas, session cursors, visibility behavior, tests, and integration
instructions are removed rather than preserved behind aliases or adapters.

See the [target CLI](docs/reference/cli.md), [implementation phases](docs/architecture/status.md),
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

Until the first clean-break phase lands, green legacy suites are implementation
regression evidence only. They do not prove the adopted product.

## License

MIT
