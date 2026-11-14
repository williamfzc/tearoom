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
