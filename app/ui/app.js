// Tearoom desktop face: state, IPC calls, room selection, and history.
// A read boundary lives only in memory; the Room store keeps no cursor.
// The composer and its send flow live in composer.js.

import { element, renderRooms, renderRoster, avatarFill, MessageList } from "./ui.js";
import { Composer } from "./composer.js";
import { TerminalDrawer } from "./terminal.js";

const { invoke } = window.__TAURI__.core;

const ui = {
  gate: document.getElementById("gate"),
  gateHint: document.getElementById("gate-hint"),
  gateError: document.getElementById("gate-error"),
  pick: document.getElementById("pick"),
  app: document.getElementById("app"),
  rooms: document.getElementById("rooms"),
  workspace: document.getElementById("workspace"),
  switch: document.getElementById("switch"),
  refresh: document.getElementById("refresh"),
  empty: document.getElementById("empty"),
  panel: document.getElementById("room-panel"),
  roomAvatar: document.getElementById("room-avatar"),
  roomName: document.getElementById("room-name"),
  roomRoles: document.getElementById("room-roles"),
  status: document.getElementById("stream-status"),
  records: document.getElementById("records"),
};

const drawer = new TerminalDrawer({
  drawer: document.getElementById("drawer"),
  resize: document.getElementById("drawer-resize"),
  avatar: document.getElementById("drawer-avatar"),
  name: document.getElementById("drawer-name"),
  status: document.getElementById("drawer-status"),
  close: document.getElementById("drawer-close"),
  term: document.getElementById("drawer-term"),
  onLayoutChange: chatScrollGuard,
});

// Only senders that are configured roles of the current room have a panel.
function openRolePanel(role) {
  if (!state.selected) return;
  drawer.show(state.selected, role);
}

// Opening or closing the drawer rewraps the chat; keep the reader's place.
function chatScrollGuard() {
  const list = ui.records;
  const anchor = {
    atBottom: list.scrollHeight - list.scrollTop - list.clientHeight < 90,
    top: list.scrollTop,
  };
  requestAnimationFrame(() => {
    if (anchor.atBottom) list.scrollTop = list.scrollHeight;
    else list.scrollTop = anchor.top;
  });
}

function isRoleSender(author) {
  const room = roomById(state.selected);
  return Boolean(room?.roles?.includes(author));
}

const composer = new Composer({
  composer: document.getElementById("composer"),
  recipients: document.getElementById("recipients"),
  recipientToggle: document.getElementById("recipient-toggle"),
  addresses: document.getElementById("addresses"),
  text: document.getElementById("text"),
  author: document.getElementById("author"),
  error: document.getElementById("compose-error"),
  send: document.getElementById("send"),
});

const state = { rooms: [], roomsKey: null, selected: null, request: 0 };
const messages = new MessageList(ui.records, ui.status, isRoleSender, openRolePanel);

/* Workspace gate ----------------------------------------------------------- */

function showGate(message) {
  ui.gateError.textContent = message || "";
  ui.gateError.hidden = !message;
  ui.gateHint.textContent = message
    ? "Tearoom could not open that workspace."
    : "Choose the folder that holds your Tearoom workspace.";
  ui.app.hidden = true;
  ui.gate.hidden = false;
}

async function pickWorkspace() {
  let result;
  try {
    result = await invoke("workspace_pick");
  } catch (error) {
    showGate(String(error));
    return;
  }
  if (!result.root) return; // the person cancelled the dialog
  state.selected = null;
  messages.reset();
  ui.gate.hidden = true;
  ui.app.hidden = false;
  await refreshState();
}

/* Rooms and history -------------------------------------------------------- */

function renderEmpty() {
  if (!state.rooms.length) {
    ui.empty.replaceChildren(
      element("p", "empty-title", "No rooms yet."),
      element(
        "p",
        "empty-hint",
        "Create one with tearoom room init --name <name>.",
      ),
    );
    return;
  }
  ui.empty.replaceChildren(element("p", "empty-hint", "Select a room"));
}

function updateRoomChrome(room) {
  ui.roomName.textContent = room.name;
  avatarFill(ui.roomAvatar, room.name);
  renderRoster(ui.roomRoles, room, openRolePanel);
  document.getElementById("text").placeholder = `Message ${room.name}`;
}

function roomById(id) {
  return state.rooms.find((room) => room.id === id) || null;
}

async function loadHistory() {
  const room = state.selected;
  if (!room) return;
  const request = ++state.request;
  const stick =
    ui.records.scrollHeight - ui.records.scrollTop - ui.records.clientHeight < 90;
  try {
    const data = await invoke("room_records", {
      room,
      after: messages.lastSeq,
    });
    if (state.selected !== room || request !== state.request) return;
    for (const record of data.records || []) {
      messages.append(record, composer.author);
    }
    if (!messages.count) messages.showEmpty();
    messages.status(`${messages.count} message${messages.count === 1 ? "" : "s"}`);
    if (stick) ui.records.scrollTop = ui.records.scrollHeight;
  } catch (error) {
    if (state.selected === room) messages.status("Read failed");
  }
}

async function selectRoom(id) {
  const room = roomById(id);
  if (!room) return;
  state.selected = id;
  state.request += 1;
  messages.reset();
  ui.panel.hidden = false;
  ui.empty.hidden = true;
  updateRoomChrome(room);
  composer.setRoom(room);
  renderRooms(ui.rooms, state.rooms, state.selected, selectRoom);
  messages.status("Reading…");
  await loadHistory();
  if (state.selected === id) composer.focus();
}

function closeRoom() {
  state.selected = null;
  state.request += 1;
  messages.reset();
  ui.panel.hidden = true;
  ui.empty.hidden = false;
  renderEmpty();
  renderRooms(ui.rooms, state.rooms, state.selected, selectRoom);
}

async function refreshState() {
  let data;
  try {
    data = await invoke("room_state");
  } catch (error) {
    showGate(String(error));
    return;
  }
  ui.workspace.textContent = data.workspace || "";
  composer.setAuthor(data.actor);
  // The roster, previews, and panel pills come from room_state; re-render
  // only when something actually changed so the polled UI stays calm.
  const key = JSON.stringify(data.rooms);
  if (key !== state.roomsKey) {
    state.roomsKey = key;
    state.rooms = Array.isArray(data.rooms) ? data.rooms : [];
    if (state.selected && !roomById(state.selected)) closeRoom();
    if (state.selected) {
      const room = roomById(state.selected);
      if (room) {
        updateRoomChrome(room);
        composer.setRoom(room);
      }
    }
    renderEmpty();
    renderRooms(ui.rooms, state.rooms, state.selected, selectRoom);
  }
  if (!state.selected && state.rooms.length === 1) {
    await selectRoom(state.rooms[0].id);
  }
}

/* Wiring ------------------------------------------------------------------- */

ui.pick.addEventListener("click", pickWorkspace);
ui.switch.addEventListener("click", async () => {
  state.selected = null;
  messages.reset();
  await pickWorkspace();
});
ui.refresh.addEventListener("click", async () => {
  await refreshState();
  await loadHistory();
});
document.getElementById("composer").addEventListener("tearoom:send", async () => {
  messages.status("Sending…");
  const outcome = await composer.send(state.selected, loadHistory);
  messages.status({
    sent: `${messages.count} message${messages.count === 1 ? "" : "s"}`,
    warning: "Saved; collaboration did not start",
    failed: "Not sent",
  }[outcome] || "");
});
document.addEventListener("visibilitychange", () => {
  if (!document.hidden) {
    refreshState().then(() => loadHistory());
  }
});
// The poll keeps records AND the room roster (roles, previews) current.
setInterval(() => {
  if (document.hidden) return;
  refreshState().then(() => loadHistory());
}, 2500);

(async () => {
  try {
    const current = await invoke("workspace_current");
    if (current.root) {
      ui.app.hidden = false;
      await refreshState();
    } else {
      showGate("");
    }
  } catch (error) {
    showGate(String(error));
  }
})();
