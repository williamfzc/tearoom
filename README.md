# Tearoom

[![Test](https://github.com/williamfzc/tearoom/actions/workflows/test.yml/badge.svg)](https://github.com/williamfzc/tearoom/actions/workflows/test.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**Stop pasting context between agent chats: shared rooms, named roles, and handoffs that survive sessions.**

Tearoom coordinates AI coding agents through shared rooms. People and agents
can read the same durable conversation, hand work to named roles, and continue
across agent sessions. Roles can use different agent CLIs and models, so a
builder, reviewer, or custom team can work together without a single provider.

Describe the collaboration you want to an agent and ask it to read
`tearoom skill`. It can configure roles and handoffs for your task. The guide
includes builder/reviewer and planner/generator/evaluator examples; roles and
responsibilities remain yours to choose.

## Try a room

Tearoom is a command-line app. Clone the repository and install it with
[Rust and Cargo](https://www.rust-lang.org/tools/install) and
[Zig 0.16](https://ziglang.org/download/):

```sh
git clone https://github.com/williamfzc/tearoom.git
cd tearoom
cargo install --path . --locked
```

Create a room and write a few messages:

```sh
mkdir tearoom-demo && cd tearoom-demo
tearoom room init --name project
tearoom send --room project --as human --text "Implement the room list."
tearoom send --room project --as dev --text "The room list is ready for review."
tearoom read --room project
```

Each message is a permanent record in the room. `read` prints the shared
history as JSONL. Rooms can form a tree when a project needs separate threads
of work.

## Hand work to an agent

Ask an agent in your project directory, for example:

> Use Tearoom to set up a builder and an independent reviewer. Use my chosen
> agent CLI and model for each role, and have the builder hand its changes to
> the reviewer. Read `tearoom skill` before configuring the room.

Tearoom includes the herdr-core terminal runtime. Use `tearoom hc` for its
commands, for example `tearoom hc --skill`; a separate `hc` install is not
needed. Herdr remains an optional interface for viewing and attaching to
terminals. Authenticate the chosen Agent CLIs and check that they start in the
room directory and report their lifecycle to the runtime. Agent and model
availability depends on the local setup. A custom coding agent with an
interactive terminal UI can be used as a role when its input and lifecycle
reporting work with the runtime; remote-machine features are outside Tearoom's
collaboration guarantees. The agent setup guide explains
[readiness and integration](docs/reference/agent-integration.md).
The runtime's built-in guide is upstream text and shows commands as `hc ...`;
when following those examples, write `tearoom hc ...`.

For example, the following optional setup uses two
[Pi](https://github.com/badlogic/pi-mono/tree/main/packages/coding-agent)
roles and Tearoom's Pi lifecycle extension. It requires Pi and a configured
provider; other Agents do not need this extension:

```sh
(set -C; tearoom skill --pi-extension > .tearoom/pi-hc-status.ts)
tearoom role init --name dev --agent-type pi --command pi \
  --arg=--extension --arg="$PWD/.tearoom/pi-hc-status.ts" \
  --instructions 'Implement the request and hand it to reviewer with --to @reviewer.'
tearoom role init --name reviewer --agent-type pi --command pi \
  --arg=--extension --arg="$PWD/.tearoom/pi-hc-status.ts" \
  --instructions 'Review the change and report a verdict in the room.'
tearoom room roles --room project --add dev,reviewer
tearoom collab run
```

Keep the dispatcher running in one terminal. In another, send a directed task:

```sh
tearoom send --room project --as human --to @dev \
  --text 'Implement the room list, then hand it to the reviewer with --to @reviewer.'
```

Only explicit recipients are woken. A name mentioned in message text is just
text. Agent commands must already be installed and signed in. Resolve the
runtime's first-use prompts in the actual room directory before expecting
unattended delivery. See the
[Agent setup guide](docs/reference/agent-integration.md) for the full setup and
recovery limits.

## What Tearoom owns

- **Rooms** hold one shared, ordered history. Anyone with access to the local
  workspace can read and write it.
- **Roles** name agent launch commands and route messages. They do not grant
  permissions or hide messages from other room readers. Signatures are labels,
  not verified identities.
- **The dispatcher** queues messages for a busy role and delivers them through
  hc. The agent decides how to understand and act on the message.

Tearoom is local first. Its workspace is a trust boundary, not a hosted service
or an access-control system. The optional `tearoom web` interface is available
only on the local machine.

For build checks, use `make check`. Read the [CLI reference](docs/reference/cli.md)
for commands, the [product definition](docs/product.md) for scope, and the
[evaluation record](docs/evaluations.md) for what has been exercised.
