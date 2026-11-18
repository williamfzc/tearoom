// Tearoom face controller: the machine-wide dashboard on the desktop, the
// paired-Mac view over the LAN face on a phone. Every Tearoom workspace
// shows its rooms side by side — there is no switching. Selection is a
// (workspace, room) pair; records, sends, and panels all take the workspace
// root explicitly. A read boundary lives only in memory. Which transport
// carries the data is decided in transport.js.

import { element, renderDashboard, renderRoster, refreshRoster, avatarFill, MessageList } from "./ui.js";
import { Composer } from "./composer.js";
import { TerminalDrawer } from "./terminal.js";
import { transport } from "./transport.js";
import { initGates } from "./gate.js";

const ui = {
  app: document.getElementById("app"),
  rooms: document.getElementById("rooms"),
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

const state = { workspaces: [], workspacesKey: null, selected: null, request: 0, poll: 0, rolesKey: null };
const messages = new MessageList(ui.records, ui.status, isRoleSender, openRolePanel);
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
      state.selected = null;
      messages.reset();
      gates.showApp();
      await refreshState();
    },
    afterConnect: async () => {
      // Let refreshState own the result: the error, the app, or the no-rooms hint.
      gates.showApp();
      ui.app.classList.remove("in-room");
      await refreshState();
    },
  },
);

/* Rooms and history -------------------------------------------------------- */

function totalRooms() {
  return state.workspaces.reduce((sum, workspace) => sum + (workspace.rooms?.length || 0), 0);
}

function selectedRoom() {
  if (!state.selected) return null;
  const workspace = state.workspaces.find((w) => w.root === state.selected.root);
  return workspace?.rooms?.find((room) => room.id === state.selected.id) || null;
}

function renderEmpty() {
  if (!totalRooms()) {
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
  renderRoster(ui.roomRoles, room, drawer ? openRolePanel : null);
  refreshSelectedRoster();
  document.getElementById("text").placeholder = `Message ${room.name}`;
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
    if (state.selected?.id === id) messages.status("Read failed");
  }
}

function selectRoom(root, room) {
  if (state.selected?.root === root && state.selected?.id === room.id) {
    ui.app.classList.add("in-room");
    return;
  }
  drawer?.close();
  state.selected = { root, id: room.id };
  state.request += 1;
  state.rolesKey = JSON.stringify(room.roles || []);
  messages.reset();
  ui.panel.hidden = false;
  ui.empty.hidden = true;
  updateRoomChrome(room);
  composer.setRoom(room, true);
  renderDashboard(ui.rooms, state.workspaces, state.selected, selectRoom);
  messages.status("Reading…");
  loadHistory();
  ui.app.classList.add("in-room");
  composer.focus();
}

function closeRoom() {
  drawer?.close();
  state.selected = null;
  state.request += 1;
  messages.reset();
  ui.panel.hidden = true;
  ui.empty.hidden = false;
  renderEmpty();
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
    // The selected room may have vanished with its workspace.
    if (state.selected && !selectedRoom()) {
      state.selected = null;
      messages.reset();
      ui.panel.hidden = true;
      renderEmpty();
    }
    if (state.selected) {
      const room = selectedRoom();
      if (room) {
        const rolesKey = JSON.stringify(room.roles || []);
        if (rolesKey !== state.rolesKey) {
          state.rolesKey = rolesKey;
          updateRoomChrome(room);
          composer.setRoom(room);
          // Re-read history so label affordances follow the new roster.
          messages.reset();
          loadHistory();
        }
      }
    }
    renderDashboard(ui.rooms, state.workspaces, state.selected, selectRoom);
    renderEmpty();
  }
  if (!state.selected && totalRooms() === 1) {
    for (const workspace of state.workspaces) {
      if (workspace.rooms?.length) {
        selectRoom(workspace.root, workspace.rooms[0]);
        break;
      }
    }
  }
  // A successful poll clears a reconnect gate; an empty workspace keeps its
  // hint instead of showing an empty dashboard.
  if (totalRooms()) gates.showApp();
  else gates.showGate("");
  refreshSelectedRoster();
  return true;
}

function refreshSelectedRoster() {
  const selection = state.selected;
  if (drawer && selection) refreshRoster(ui.roomRoles, selection, () => state.selected === selection);
}

/* Wiring ------------------------------------------------------------------- */

ui.refresh.addEventListener("click", async () => {
  await refreshState();
  await loadHistory();
});
document.getElementById("back").addEventListener("click", closeRoom);
document.getElementById("composer").addEventListener("tearoom:send", async () => {
  if (!state.selected) return;
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
