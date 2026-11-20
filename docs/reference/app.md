---
type: reference
title: App contract
description: The optional Tauri faces — the desktop window over local workspaces and the phone window over the LAN face.
tags: [reference, app, desktop, ios, contract]
timestamp: 2026-11-19
---

# App contract

The app (`app/`, one Tauri crate, two faces) is the optional windowed view of
Tearoom: it navigates the Room tree, reads a Room's ordered record stream,
and appends text messages with explicit `@role` addresses. It is a view over
the same Store and message rules as the CLI; it adds no entity, identity, or
remote-serving boundary. The product statement is in
[product.md](../product.md#command-surface); the browser face it replaced is
recorded in [../log.md](../log.md).

The two faces share the UI (renderers, composer, conversation) and differ
only in the transport behind one seam (`app/ui/transport.js`): the desktop
face talks to the local Store and CLI through Tauri commands; the phone face
talks to the paired Mac's [LAN face](lan-face.md) over fetch.

## Behavior

- **Reads** go through the Tearoom Store directly (`Store::open`,
  `resolve_room`, `read`). Reads never start the collaboration service and
  persist no cursor; the read boundary lives in the window only.
- **Sends** run the CLI `send` command as a subprocess of the selected
  workspace. Message composition, roster validation, mentions, and service
  activation therefore have one owner (the CLI path). If the record was
  saved but the service did not start, the app shows the CLI's
  "record saved" warning instead of a failure.
- **Composer** — the Mac uses `TEAROOM_ACTOR` when set, then the macOS
  account name, with `me` as a fallback. The phone keeps a previously saved
  sender name or uses `me`. Type `@` to find a role in the current Room and
  select it to add a removable recipient. Selected roles become explicit
  delivery targets. The app remembers draft text and recipients together per
  workspace and Room while the window is open. Text left in the message body
  follows the [ordinary addressing rule](cli.md#append-and-read).
- **Workspace discovery** — the dashboard shows every Tearoom workspace on
  this machine side by side. Discovery merges the collaboration service's
  registry, Spotlight, a bounded home-directory scan, and the app's folder
  history. It validates each candidate by opening the Store. Selecting a Room
  selects its workspace for reads, sends, and panel attach. With no discovered
  workspace, the app offers a folder picker and shows the command for creating
  the first Room. A discovered workspace with no Rooms shows the creation hint.
- **Room list** — collapse a workspace or a Room's child branch to focus the
  list. Search filters the list immediately and shows matches inside collapsed
  branches. Clearing the search restores the prior
  collapse state. On narrow
  screens, Back leaves the Room list open through subsequent refreshes. On
  reopening, the app restores the last selected workspace and Room if it finds
  them and reads their saved history from the beginning.
- **CLI discovery** — sends need the `tearoom` binary: `TEAROOM_CLI` if set,
  otherwise `tearoom` on `PATH`, `~/.cargo/bin`, `~/.local/bin`,
  `/opt/homebrew/bin`, or `/usr/local/bin`. The child gets an extended
  `PATH` so the service and Agent terminals it starts later resolve tools
  the way a person's shell would.
- **Conversation view** — records render as group-chat clusters grouped by
  author; the person's own messages (matching the current sender) align
  right. Mentions are highlighted, addressed messages show a
  `→ @role` route line, and `reply_to` references render as `↩ room#seq`.
  Content that is neither plain text nor a message object renders verbatim.
  New records arrive by polling with an explicit `after` boundary.

## Role panel drawer

Clicking a sender that is a configured role of the Room — its name above a
bubble, the `▸` expand button under one of its messages, or its avatar in the
role strip — opens an inline terminal attached to that role's
panel. The stream is herdr-core's framed `terminal.attach` over the hc
socket (the same bytes Herdr's panes show), wrapped by `tearoom::hc::attach`;
the webview renders it with the vendored xterm.js.

- **Interactive, like a Herdr pane**: typing goes to the agent's PTY, so
  first-use prompts can be resolved without leaving the app. herdr-core
  allows several viewers per panel; the most recently active one owns the
  size. Closing the drawer detaches the viewer; the panel keeps running.
- **The drawer docks when there is room for both the terminal and chat**.
  At narrower widths it covers the conversation until closed. In the docked
  view, drag the drawer's left edge to resize it. The app remembers the width
  and fits it to the available space. The divider snaps when the conversation
  and terminal approach equal widths, and its indicator yields slightly at
  the width limits. Reduced Motion removes the indicator movement. The
  terminal relayouts live and both scroll positions are preserved.
- The desktop role strip shows avatars and status dots. Hover or focus an
  avatar to see the role name and full status.
  It checks each role as soon as the Room opens,
  then refreshes each result independently. A live panel without a lifecycle
  report shows `No status reported`. `No active panel` means hc has no live
  panel for the role, and `Status unavailable` means the lookup failed.
  A brief pulse marks a role entering Working. Reduced Motion keeps the
  same status cues static.
  The drawer uses the same labels and connects if a panel appears.
  `Turn ended` describes the Agent's turn. Judge the work from its evidence.
- The drawer explains when it is waiting for output, when no panel is live,
  and when a connection fails. It stays reachable at narrow desktop widths.
  A previously used missing panel can require reconciliation. See the
  [delivery contract](../architecture/collaboration.md#conversation-continuity-and-replacement).
- This is a view over an existing Role's runtime state — no new entity, no
  admission change; whoever can reach the hc socket can already attach.

## What it refuses

- No editing of records, moving of Rooms, or room creation: setup stays with
  the CLI (`tearoom room init`, `role init`).
- No network serving from the desktop window: it is a client of the local
  Store and CLI, never a server. The phone face is likewise a client only —
  its serving counterpart is the paired Mac's LAN face (see
  [lan-face.md](lan-face.md)).
- No signature verification: the author remains an unverified label.

## The phone face (iOS)

The same crate builds for iOS (`cargo tauri ios …`), where the Rust command
surface is compiled out entirely: the face is a bare webview whose UI runs
the remote transport against the paired Mac's LAN face. First run shows the
connect gate — the address and six-digit code from `tearoom collab pair` —
and a successful pairing stores the endpoint and token locally. After that
it behaves like the desktop conversation with these differences:

- One workspace (the paired one); discovery, the folder picker, and the
  workspace picker gate do not exist on the phone.
- The role-panel drawer does not exist: the LAN face carries no terminal
  stream, so role pills render as static text without the attach affordance.
  The phone webview boots without the Tauri global; terminal commands resolve
  only when the desktop drawer is used.
- Reads poll `records` with the same explicit `after` boundary at the same
  cadence as the desktop. Sends go through `/v1/send` with a phone-generated
  idempotency key scoped to the exact Room, text, author, and recipient set
  (recipient order is canonical); the key is kept locally until the send is
  confirmed or refused, and unconfirmed keys are not silently evicted.
  Retrying an unchanged payload after a lost response therefore replays the
  saved record, while changing any of those fields gets a fresh key and
  cannot turn an edit into an idempotency conflict. A confirmed send forgets
  its key, so sending the same message again appends a new record. A network
  failure and a 5xx response are uncertain; a 4xx refusal is definite. A
  saved record whose wake failed returns as a 200 warning carrying the
  record, and the face shows the same saved-with-warning message as the
  desktop; a genuinely refused send shows the face's error.
- A failed poll shows a reconnect hint without discarding the selected Room;
  a later successful poll restores the conversation and reads from its
  in-memory boundary. A poll that fails immediately after pairing keeps its
  network error instead of being replaced by the empty-workspace hint. Only
  the newest poll applies: a slow older response cannot hide a newer success
  or clear a newer failure gate.
- A revoked token (or a Mac you no longer trust) offers Pair again on the
  gate; it forgets the stored endpoint and token and returns to the connect
  screen. The selected Room and history are left for the next pairing.
- The threat model is the LAN face's ([lan-face.md](lan-face.md)); the
  token stays on the device.

Distribution is personal sideloading; packaging and store distribution are
pending and tracked in [../architecture/status.md](../architecture/status.md).

## Build and run

The app needs the same toolchain as the CLI (Rust and Zig 0.16, see
[building](../operations/building.md)); the Tauri dependencies come from
Cargo. From the repository root:

```sh
cargo run --manifest-path app/Cargo.toml        # development window
cargo build --locked --manifest-path app/Cargo.toml
cargo tauri ios build --target aarch64-sim      # phone face, simulator
```

The first iOS scaffold is `cargo tauri ios init` inside `app/`. Its
generated project lives under `app/gen/apple/` — deliberately untracked
like the rest of `gen/`, since `tauri ios init` reproduces it. One manual
step does not survive regeneration and must be reapplied after a fresh
`init`: `gen/apple/tearoom-app_iOS/Info.plist` carries the LAN-face keys —
`NSAppTransportSecurity` → `NSAllowsLocalNetworking` (the face speaks plain
HTTP to the paired Mac) and `NSLocalNetworkUsageDescription` (iOS asks
before touching the local network). The webview's CSP for iOS comes from
the checked-in `app/tauri.ios.conf.json`.

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
