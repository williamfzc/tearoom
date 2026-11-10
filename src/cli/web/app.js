// Local browser face: state, API calls, room selection, and sending.
// A read boundary lives only in memory; the Room store keeps no cursor.

import {
  element,
  MessageView,
  renderAddresses,
  renderRooms,
} from "./ui.js";

(() => {
  "use strict";

  const tokenMeta = document.querySelector('meta[name="tearoom-token"]');
  const token = tokenMeta ? tokenMeta.content : "";
  const ui = {
    rooms: document.getElementById("rooms"),
    roomCount: document.getElementById("room-count"),
    roomStatus: document.getElementById("room-status"),
    empty: document.getElementById("empty"),
    panel: document.getElementById("room-panel"),
    back: document.getElementById("back"),
    roomName: document.getElementById("room-name"),
    roomMeta: document.getElementById("room-meta"),
    records: document.getElementById("records"),
    composer: document.getElementById("composer"),
    text: document.getElementById("text"),
    author: document.getElementById("author"),
    recipients: document.getElementById("recipients"),
    addresses: document.getElementById("addresses"),
    error: document.getElementById("compose-error"),
    send: document.getElementById("send"),
    refresh: document.getElementById("refresh"),
  };

  const state = {
    rooms: [],
    selected: null,
    request: 0,
    sending: false,
  };
  const messages = new MessageView(ui.records, document.getElementById("stream-status"));

  async function api(path, options = {}) {
    const headers = new Headers(options.headers || {});
    headers.set("X-Tearoom-Token", token);
    if (options.body) headers.set("Content-Type", "application/json");
    const response = await fetch(path, {
      ...options,
      headers,
      cache: "no-store",
      credentials: "same-origin",
    });
    const type = response.headers.get("content-type") || "";
    const payload = type.includes("application/json")
      ? await response.json()
      : await response.text();
    if (!response.ok) {
      const detail =
        payload && typeof payload === "object" && payload.error
          ? payload.error
          : typeof payload === "string" && payload.trim()
            ? payload.trim()
            : `HTTP ${response.status}`;
      throw new Error(detail);
    }
    return payload;
  }

  function setRoomStatus(text) {
    ui.roomStatus.textContent = text;
  }

  function setError(text) {
    ui.error.textContent = text || "";
    ui.error.hidden = !text;
  }

  function roomByName(name) {
    return state.rooms.find((room) => room.name === name) || null;
  }

  function updateRoomChrome(room) {
    ui.roomName.textContent = room.name;
    ui.roomMeta.textContent = room.parent ? `in ${room.parent}` : "root room";
    ui.text.placeholder = `Message ${room.name}`;
  }

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

  function selectedRoles() {
    return new Set(
      Array.from(
        ui.addresses.querySelectorAll('input[name="address"]:checked'),
      ).map((input) => input.value),
    );
  }

  async function loadHistory(initial) {
    const room = state.selected;
    if (!room) return;
    const request = ++state.request;
    const list = ui.records;
    const stick =
      initial || list.scrollHeight - list.scrollTop - list.clientHeight < 90;
    try {
      const data = await api(
        `/api/rooms/${encodeURIComponent(room)}/records?after=e${messages.lastSeq}`,
      );
      if (state.selected !== room || request !== state.request) return;
      for (const record of data.records || []) messages.append(record);
      if (!messages.count) messages.showEmpty();
      const count = messages.count;
      messages.status(`${count} message${count === 1 ? "" : "s"}`);
      if (stick) list.scrollTop = list.scrollHeight;
    } catch (error) {
      if (state.selected === room) {
        messages.status("Read failed");
        setError(error.message);
      }
    }
  }

  async function selectRoom(name) {
    const room = roomByName(name);
    if (!room) return;
    state.selected = name;
    state.request += 1;
    messages.reset();
    ui.panel.hidden = false;
    ui.empty.hidden = true;
    document.body.classList.add("room-open");
    updateRoomChrome(room);
    renderAddresses(ui.addresses, ui.recipients, room);
    renderRooms(ui.rooms, ui.roomCount, state.rooms, state.selected, selectRoom);
    setError("");
    messages.status("Reading…");
    await loadHistory(true);
    if (state.selected === name) {
      if (!window.matchMedia("(max-width: 700px)").matches) ui.text.focus();
      resizeText();
    }
  }

  function closeRoom() {
    state.selected = null;
    state.request += 1;
    messages.reset();
    document.body.classList.remove("room-open");
    ui.panel.hidden = true;
    ui.empty.hidden = false;
    setError("");
    renderEmpty();
    renderRooms(ui.rooms, ui.roomCount, state.rooms, state.selected, selectRoom);
  }

  async function refreshState() {
    try {
      const data = await api("/api/state");
      state.rooms = Array.isArray(data.rooms) ? data.rooms : [];
      if (!ui.author.value && data.actor) {
        ui.author.value = data.actor;
      }
      if (state.selected && !roomByName(state.selected)) {
        closeRoom();
      }
      if (state.selected) {
        const room = roomByName(state.selected);
        if (room) {
          const selected = selectedRoles();
          updateRoomChrome(room);
          renderAddresses(ui.addresses, ui.recipients, room, selected);
        }
      }
      setRoomStatus("");
      renderEmpty();
      renderRooms(ui.rooms, ui.roomCount, state.rooms, state.selected, selectRoom);
      const mobile = window.matchMedia("(max-width: 700px)").matches;
      if (!state.selected && state.rooms.length === 1 && !mobile) {
        await selectRoom(state.rooms[0].name);
      }
    } catch (error) {
      setRoomStatus(`Cannot load rooms: ${error.message}`);
    }
  }

  function resizeText() {
    ui.text.style.height = "auto";
    ui.text.style.height = `${Math.min(ui.text.scrollHeight, 132)}px`;
  }

  function updateSendState() {
    ui.send.disabled =
      state.sending || ui.text.value.trim().length === 0 || !state.selected;
  }

  async function sendMessage(event) {
    event.preventDefault();
    if (!state.selected || state.sending) return;
    const text = ui.text.value;
    if (!text.trim()) {
      setError("Write a message first.");
      ui.text.focus();
      return;
    }
    const to = Array.from(
      ui.addresses.querySelectorAll('input[name="address"]:checked'),
    ).map((input) => input.value);
    const body = { text, to };
    if (ui.author.value.trim()) body.author = ui.author.value.trim();

    state.sending = true;
    updateSendState();
    setError("");
    messages.status("Sending…");
    try {
      await api(`/api/rooms/${encodeURIComponent(state.selected)}/messages`, {
        method: "POST",
        body: JSON.stringify(body),
      });
      ui.text.value = "";
      resizeText();
      updateSendState();
      await loadHistory(false);
      ui.text.focus();
    } catch (error) {
      setError(error.message);
      messages.status("Not sent");
    } finally {
      state.sending = false;
      updateSendState();
    }
  }

  ui.composer.addEventListener("submit", sendMessage);
  ui.refresh.addEventListener("click", async () => {
    await refreshState();
    if (state.selected) await loadHistory(false);
  });
  ui.back.addEventListener("click", closeRoom);
  ui.author.addEventListener("input", () => {
    try {
      localStorage.setItem("tearoom.author", ui.author.value);
    } catch {
      /* A private browser session may refuse storage; the field still works. */
    }
  });
  ui.text.addEventListener("input", () => {
    resizeText();
    updateSendState();
  });
  ui.text.addEventListener("keydown", (event) => {
    if (event.key === "Enter" && !event.shiftKey && !event.isComposing) {
      event.preventDefault();
      if (!ui.send.disabled) ui.composer.requestSubmit();
    }
  });
  document.addEventListener("visibilitychange", () => {
    if (!document.hidden && state.selected) loadHistory(false);
  });

  try {
    ui.author.value = localStorage.getItem("tearoom.author") || "";
  } catch {
    ui.author.value = "";
  }
  updateSendState();
  refreshState();
  setInterval(() => {
    if (!document.hidden && state.selected) loadHistory(false);
  }, 2500);
})();
