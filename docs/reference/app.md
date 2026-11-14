---
type: reference
title: Desktop app contract
description: The optional Tauri desktop face — what it shows, how it reads, how it sends, and its limits.
tags: [reference, app, desktop, contract]
timestamp: 2026-11-14
---

# Desktop app contract

The desktop app (`app/`, a Tauri crate) is the optional local face: one
native window that navigates the Room tree, reads a Room's ordered record
stream, and appends text messages with explicit `@role` addresses. It is a
view over the same Store and message rules as the CLI; it adds no entity,
identity, or remote-serving boundary. The product statement is in
[product.md](../product.md#command-surface); the browser face it replaced is
recorded in [../log.md](../log.md).

## Behavior

- **Reads** go through the Tearoom Store directly (`Store::open`,
  `resolve_room`, `read`). Reads never start the collaboration service and
  persist no cursor; the read boundary lives in the window only.
- **Sends** run the CLI `send` command as a subprocess of the selected
  workspace. Message composition, roster validation, mentions, and service
  activation therefore have one owner (the CLI path). If the record was
  saved but the service did not start, the app shows the CLI's
  "record saved" warning instead of a failure.
- **Workspace** — the app owns no workspace. On first run (or when the
  remembered folder no longer opens as a workspace) it asks for a folder,
  validates it by opening the Store, and remembers the workspace directory
  in the app config dir. Any folder inside a workspace is accepted; the
  canonical workspace directory is stored.
- **CLI discovery** — sends need the `tearoom` binary: `TEAROOM_CLI` if set,
  otherwise `tearoom` on `PATH`, `~/.cargo/bin`, `~/.local/bin`,
  `/opt/homebrew/bin`, or `/usr/local/bin`. The child gets an extended
  `PATH` so the service and Agent terminals it starts later resolve tools
  the way a person's shell would.
- **Conversation view** — records render as group-chat clusters grouped by
  author; the person's own messages (matching the composer name) align
  right. Mentions are highlighted, addressed messages show a
  `→ @role` route line, and `reply_to` references render as `↩ room#seq`.
  Content that is neither plain text nor a message object renders verbatim.
  New records arrive by polling with an explicit `after` boundary.

## Role panel drawer

Clicking a sender that is a configured role of the Room — its name above a
bubble, the `▸` expand button under one of its messages, or its pill in the
`with @role` roster — opens an inline terminal attached to that role's
panel. The stream is herdr-core's framed `terminal.attach` over the hc
socket (the same bytes Herdr's panes show), wrapped by `tearoom::hc::attach`;
the webview renders it with the vendored xterm.js.

- **Interactive, like a Herdr pane**: typing goes to the agent's PTY, so
  first-use prompts can be resolved without leaving the app. herdr-core
  allows several viewers per panel; the most recently active one owns the
  size. Closing the drawer detaches the viewer; the panel keeps running.
- **The drawer's left edge drags to resize**, and the width is remembered.
  Resizing forwards to the panel, so the agent's TUI relayouts live. Both
  scroll views are protected: resizing or opening the drawer never yanks a
  scrolled-up viewport — the terminal's place returns after a relayout, and
  the chat list keeps its position when the drawer rewraps it.
- **No live panel** shows a hint instead: wake the role with an addressed
  message first.
- This is a view over an existing Role's runtime state — no new entity, no
  admission change; whoever can reach the hc socket can already attach.

## What it refuses

- No editing of records, moving of Rooms, or room creation: setup stays with
  the CLI (`tearoom room init`, `role init`).
- No network serving of any kind: the window is not a server, and there is
  no remote mode. This is not a hosted control plane.
- No signature verification: the author remains an unverified label.

## Build and run

The app needs the same toolchain as the CLI (Rust and Zig 0.16, see
[building](../operations/building.md)); the Tauri dependencies come from
Cargo. From the repository root:

```sh
cargo run --manifest-path app/Cargo.toml        # development window
cargo build --locked --manifest-path app/Cargo.toml
```

The icon source is `app/icons/icon.svg`, drawn on the macOS icon grid
(1024 canvas, 824 rounded-square body, r=185). Render it with `resvg` —
`qlmanage` thumbnails flatten the transparent background onto white — then
build the `icon.icns` set with `iconutil`, and force a rebuild so the binary
re-embeds the icon.

Signed and notarized bundles are not yet shipped; packaging is pending and
tracked in [../architecture/status.md](../architecture/status.md).

## Verification

Command behavior (state, reads, send argument building, the saved-record
warning, and CLI discovery) is unit-tested in `app/src/commands.rs` under
`make app-test`. The window journey itself cannot run in `make check`; it is
hand-run evidence in [../evaluations.md](../evaluations.md).
