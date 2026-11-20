// Tearoom face controller: selection by workspace and Room, polling, and sending
// for the local desktop and paired-Mac app.

import { renderRoster, refreshRoster, avatarFill, MessageList } from "./ui.js";
import { RoomList } from "./room-list.js";
import { Composer } from "./composer.js";
import { TerminalDrawer } from "./terminal.js";
import { transport } from "./transport.js";
import { initGates } from "./gate.js";

const ui = {
  app: document.getElementById("app"),
  refresh: document.getElementById("refresh"),
  empty: document.getElementById("empty"),
  panel: document.getElementById("room-panel"),
  roomAvatar: document.getElementById("room-avatar"),
  roomName: document.getElementById("room-name"),
  roomRoles: document.getElementById("room-roles"),
  status: document.getElementById("stream-status"),
  records: document.getElementById("records"),
};

const drawer = transport.capabilities.terminal
  ? new TerminalDrawer({
      drawer: document.getElementById("drawer"),
      resize: document.getElementById("drawer-resize"),
      avatar: document.getElementById("drawer-avatar"),
      name: document.getElementById("drawer-name"),
      status: document.getElementById("drawer-status"),
      close: document.getElementById("drawer-close"),
      feedback: document.getElementById("drawer-feedback"),
      term: document.getElementById("drawer-term"),
      onLayoutChange: chatScrollGuard,
    })
  : null;

const composer = new Composer({
  composer: document.getElementById("composer"),
  suggestions: document.getElementById("mention-options"),
  selected: document.getElementById("selected-recipients"),
  text: document.getElementById("text"),
  error: document.getElementById("compose-error"),
  send: document.getElementById("send"),
});

const state = { workspaces: [], workspacesKey: null, selected: null, request: 0, poll: 0, rolesKey: null, autoOpen: true };
const messages = new MessageList(ui.records, ui.status, isRoleSender, openRolePanel);
const roomList = new RoomList(document.getElementById("rooms"), document.getElementById("room-search"), selectRoom);
/* Boot gates: the desktop workspace picker and the phone's connect screen. */
const gateCard = document.getElementById("gate");
const connectCard = document.getElementById("connect");
const gates = initGates(
  {
    app: ui.app,
    gate: gateCard,
    gateHint: document.getElementById("gate-hint"),
    gateError: document.getElementById("gate-error"),
    pick: document.getElementById("pick"),
    reconnect: document.getElementById("reconnect"),
    connect: connectCard,
    connectForm: document.getElementById("connect-form"),
    connectAddress: document.getElementById("connect-address"),
    connectCode: document.getElementById("connect-code"),
    connectError: document.getElementById("connect-error"),
    connectSubmit: document.getElementById("connect-submit"),
  },
  transport,
  {
    afterPick: async () => {
      closeRoom();
      state.workspacesKey = null;
      gates.showApp();
      await refreshState();
    },
    afterConnect: async () => {
      closeRoom();
      composer.clearDrafts();
      rememberRoom(null);
      state.autoOpen = true;
      state.workspacesKey = null;
      gates.showApp();
      await refreshState();
    },
  },
);

/* Rooms and history -------------------------------------------------------- */

function totalRooms() {
  return state.workspaces.reduce((sum, workspace) => sum + (workspace.rooms?.length || 0), 0);
}

function selectedRoom(selection = state.selected) {
  const workspace = selection && state.workspaces.find((w) => w.root === selection.root);
  return workspace?.rooms?.find((room) => room.id === selection.id) || null;
}

const ROOM_SELECTION = "tearoom.selectedRoom";
function rememberRoom(selection) {
  try {
    localStorage.setItem(ROOM_SELECTION, JSON.stringify(selection));
  } catch { /* Storage may be unavailable. */ }
}
function restoreRoom() {
  let selection;
  try {
    selection = JSON.parse(localStorage.getItem(ROOM_SELECTION) || "null");
  } catch { return; }
  const room = selectedRoom(selection);
  if (room) selectRoom(selection.root, room);
}

function updateRoomChrome(room) {
  ui.roomName.textContent = room.name;
  avatarFill(ui.roomAvatar, room.name);
  renderRoster(ui.roomRoles, room, drawer ? openRolePanel : null);
  refreshSelectedRoster();
}

function isRoleSender(author) {
  if (!drawer) return false;
  const room = selectedRoom();
  return Boolean(room?.roles?.includes(author));
}
function openRolePanel(role) {
  if (!state.selected || !drawer) return;
  drawer.show(state.selected.root, state.selected.id, role);
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

async function loadHistory() {
  if (!state.selected) return;
  const { root, id } = state.selected;
  const request = ++state.request;
  const stick =
    ui.records.scrollHeight - ui.records.scrollTop - ui.records.clientHeight < 90;
  try {
    const data = await transport.records(root, id, messages.lastSeq);
    if (state.selected?.id !== id || state.selected?.root !== root || request !== state.request) {
      return;
    }
    for (const record of data.records || []) {
      messages.append(record, composer.author);
    }
    if (!messages.count) messages.showEmpty();
    messages.status(`${messages.count} message${messages.count === 1 ? "" : "s"}`);
    if (stick) ui.records.scrollTop = ui.records.scrollHeight;
  } catch (error) {
    if (request === state.request) messages.status("Read failed");
  }
}

function selectRoom(root, room) {
  state.autoOpen = false;
  drawer?.close();
  if (state.selected?.root === root && state.selected?.id === room.id) {
    ui.app.classList.add("in-room");
    return;
  }
  state.selected = { root, id: room.id };
  rememberRoom(state.selected);
  state.request += 1;
  state.rolesKey = JSON.stringify(room.roles || []);
  messages.reset();
  ui.panel.hidden = false;
  ui.empty.hidden = true;
  updateRoomChrome(room);
  composer.setRoom(state.selected, room);
  roomList.render(state.workspaces, state.selected);
  messages.status("Reading…");
  loadHistory();
  ui.app.classList.add("in-room");
  composer.focus();
}

function closeRoom() {
  drawer?.close();
  state.selected = null;
  composer.setRoom(null);
  state.request += 1;
  messages.reset();
  ui.panel.hidden = true;
  ui.empty.hidden = false;
  ui.app.classList.remove("in-room");
}

async function refreshState() {
  const request = ++state.poll;
  // The poll and visibility handler run before pairing too; keep the
  // connect screen up until a token exists.
  if (transport.kind === "remote" && !transport.connected()) {
    gates.showConnect();
    return false;
  }
  let data;
  try {
    data = await transport.listWorkspaces();
  } catch (error) {
    if (request === state.poll) gates.showGate(String(error));
    return false;
  }
  if (request !== state.poll) return false;
  composer.setAuthor(data.actor);
  const workspaces = Array.isArray(data.workspaces) ? data.workspaces : [];
  const key = JSON.stringify(workspaces);
  if (key !== state.workspacesKey) {
    state.workspacesKey = key;
    state.workspaces = workspaces;
    const room = selectedRoom();
    // The selected room may have vanished with its workspace.
    if (state.selected && !room) closeRoom();
    if (room) {
      const rolesKey = JSON.stringify(room.roles || []);
      if (rolesKey !== state.rolesKey) {
        state.rolesKey = rolesKey;
        updateRoomChrome(room);
        composer.setRoom(state.selected, room);
        // Re-read history so label affordances follow the new roster.
        messages.reset();
        loadHistory();
      }
    }
    roomList.render(state.workspaces, state.selected);
    if (state.autoOpen) restoreRoom();
  }
  if (state.autoOpen && totalRooms() === 1) {
    for (const workspace of state.workspaces) {
      if (workspace.rooms?.length) {
        selectRoom(workspace.root, workspace.rooms[0]);
        break;
      }
    }
  }
  if (totalRooms()) gates.showApp();
  else gates.showGate("", workspaces.length > 0);
  refreshSelectedRoster();
  return true;
}

function refreshSelectedRoster() {
  const selection = state.selected;
  if (drawer && selection) refreshRoster(ui.roomRoles, selection, () => state.selected === selection,
    (states) => composer.setRoleStates(states));
}

/* Wiring ------------------------------------------------------------------- */

ui.refresh.addEventListener("click", async () => {
  await refreshState();
  await loadHistory();
});
document.getElementById("back").addEventListener("click", () => {
  const previous = state.selected;
  closeRoom();
  rememberRoom(null);
  roomList.render(state.workspaces, state.selected);
  roomList.focus(previous);
});
document.getElementById("composer").addEventListener("tearoom:send", async () => {
  const selection = state.selected;
  if (!selection) return;
  messages.status("Sending…");
  const outcome = await composer.send(selection, loadHistory);
  if (state.selected !== selection) return;
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
setInterval(() => {
  if (document.hidden) return;
  refreshState().then(() => loadHistory());
}, 2500);

(async () => {
  if (transport.kind === "remote" && !transport.connected()) {
    gates.showConnect();
    return;
  }
  gates.showApp();
  await refreshState();
})();
