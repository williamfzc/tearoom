// Room navigation for the shared app rail: workspace groups, room trees,
// search, and the remembered selection across reopenings.

import { avatarFill, element, ageLabel } from "./ui.js";

const ROOM_SELECTION = "tearoom.selectedRoom";
export function rememberRoom(selection) {
  try {
    localStorage.setItem(ROOM_SELECTION, JSON.stringify(selection));
  } catch { /* Storage may be unavailable. */ }
}

export function restoreRoom(workspaces, onRestore) {
  let selection;
  try {
    selection = JSON.parse(localStorage.getItem(ROOM_SELECTION) || "null");
  } catch { return; }
  const workspace = selection && workspaces.find((w) => w.root === selection.root);
  const room = workspace?.rooms?.find((room) => room.id === selection.id);
  if (room) onRestore(selection.root, room);
}

function roomPreview(room) {
  const last = room.last;
  if (typeof last?.text !== "string" || !last.text) return room.parent ? "" : room.description || "";
  const author = typeof last.author === "string" ? last.author : "";
  return author ? `${author}: ${last.text}` : last.text;
}

function roomButton(room, selected) {
  const button = element("button", "room-item");
  button.type = "button";
  if (selected) button.setAttribute("aria-current", "true");
  button.title = room.description || room.name;
  const avatar = element("span", "avatar");
  avatar.setAttribute("aria-hidden", "true");
  avatarFill(avatar, room.name);
  const text = element("span", "row-text");
  const top = element("span", "row-top");
  top.append(element("span", "row-name", room.name));
  const time = room.last ? ageLabel(room.last.at) : "";
  if (time) top.append(element("time", "row-time", time));
  text.append(top, element("span", "row-preview", roomPreview(room)));
  button.append(avatar, text);
  return button;
}

function roomKey(root, id) {
  return JSON.stringify([root, id]);
}

export class RoomList {
  constructor(container, search, onSelect) {
    this.container = container;
    this.input = search.elements.namedItem("query");
    this.clear = search.elements.namedItem("clear");
    this.status = search.querySelector(".room-search-status");
    this.onSelect = onSelect;
    this.collapsed = new Set();
    this.workspaces = [];
    this.selected = null;
    search.addEventListener("input", () => this.updateSearch());
    search.addEventListener("submit", (event) => {
      event.preventDefault();
      if (this.input.value.trim()) this.container.querySelector(".room-item")?.click();
    });
    this.clear.addEventListener("click", () => this.clearSearch());
    search.addEventListener("keydown", (event) => {
      if (event.key === "Escape" && !event.isComposing && this.input.value) {
        event.preventDefault();
        this.clearSearch();
      }
    });
  }

  updateSearch() {
    this.container.scrollTop = 0;
    this.render(this.workspaces, this.selected);
  }

  clearSearch() {
    this.input.value = "";
    this.updateSearch();
    this.input.focus();
  }

  button(key) {
    return this.container.querySelector(`[data-rail-key="${CSS.escape(key)}"]`);
  }

  focus(selection = this.selected) {
    const previous = selection && this.button(roomKey(selection.root, selection.id));
    (previous || this.container.querySelector(".room-item") || this.input).focus();
  }

  disclosure(button, key, group) {
    const expanded = !this.collapsed.has(key);
    group.id = `room-list-${encodeURIComponent(key)}`;
    group.hidden = !expanded;
    button.type = "button";
    button.dataset.railKey = key;
    button.setAttribute("aria-controls", group.id);
    button.setAttribute("aria-expanded", String(expanded));
    const arrow = element("span", "rail-chevron", "›");
    arrow.setAttribute("aria-hidden", "true");
    button.prepend(arrow);
    button.addEventListener("click", () => {
      if (expanded) this.collapsed.add(key);
      else this.collapsed.delete(key);
      this.render(this.workspaces, this.selected);
    });
    return expanded;
  }

  render(workspaces, selected) {
    this.workspaces = workspaces;
    this.selected = selected;
    const active = document.activeElement;
    const focusKey = this.container.contains(active) && active.dataset.railKey;
    const scrollTop = this.container.scrollTop;
    const query = this.input.value.trim().toLowerCase();
    this.clear.hidden = !this.input.value;
    const sections = [];
    for (const workspace of workspaces) {
      const rooms = workspace.rooms || [];
      const matches = query ? rooms.filter((room) => room.name.toLowerCase().includes(query)) : rooms;
      if (query && !matches.length) continue;
      sections.push(this.workspaceSection(workspace, matches, Boolean(query)));
    }
    const status = query && !sections.length ? "No rooms match your search." : "";
    if (this.status.textContent !== status) this.status.textContent = status;
    this.container.replaceChildren(...sections);
    this.container.scrollTop = scrollTop;
    if (focusKey) (this.button(focusKey) || this.input).focus({ preventScroll: true });
  }

  workspaceSection(workspace, rooms, searching) {
    const { root } = workspace;
    const name = workspace.name || root;
    const section = element("section", "ws");
    section.setAttribute("aria-label", name);
    const collapsible = !searching && rooms.length > 0;
    const head = element(collapsible ? "button" : "h2", "ws-head");
    head.title = root;
    head.append(element("span", "ws-name", name), element("span", "ws-count", String(rooms.length)));
    const group = element("div");
    section.append(head, group);
    if (collapsible && !this.disclosure(head, roomKey(root, null), group)) return section;

    const byParent = new Map();
    for (const room of rooms) {
      const parent = searching ? "" : room.parent;
      if (!byParent.has(parent)) byParent.set(parent, []);
      byParent.get(parent).push(room);
    }
    const appendRooms = (container, parent, depth) => {
      for (const room of byParent.get(parent) || []) {
        const key = roomKey(root, room.id);
        const item = roomButton(room, this.selected?.root === root && this.selected.id === room.id);
        item.dataset.railKey = key;
        item.addEventListener("click", () => this.onSelect(root, room));
        const row = element("div", "room-row");
        row.style.paddingLeft = `${Math.min(depth, 4) * 12}px`;
        row.append(item);
        container.append(row);
        if (!searching && byParent.has(room.id)) {
          const children = element("div");
          const toggle = element("button", "room-toggle");
          toggle.setAttribute("aria-label", `Subrooms of ${room.name}`);
          row.prepend(toggle);
          container.append(children);
          if (this.disclosure(toggle, `${key}:subrooms`, children)) appendRooms(children, room.id, depth + 1);
        }
      }
    };
    appendRooms(group, "", 0);
    return section;
  }
}
