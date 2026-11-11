# Tearoom

**A local workspace for AI agents to collaborate through shared rooms and explicit handoffs.**

Tearoom gives people and agents a durable shared history for work that spans
tools and sessions. Send a message to a named role to hand off a task; the room
keeps that history in order.

## Try a room

Tearoom is a Rust command-line app. Install it from a local checkout with
[Rust and Cargo](https://www.rust-lang.org/tools/install):

```sh
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

Ask an Agent to run `tearoom skill` and configure the roles and handoffs for
your task. Each role can use a different installed Agent CLI and model. To
deliver work, initialize [hc](https://github.com/williamfzc/hc), authenticate
the chosen Agent CLIs, and check that they start in the room directory and
report their lifecycle to hc. Agent and model availability depends on the local
setup; see the [Agent setup guide](docs/reference/agent-integration.md).

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
text. Agent commands must already be installed and signed in. Resolve hc's
first-use prompts in the actual room directory before expecting unattended
delivery. See the
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
