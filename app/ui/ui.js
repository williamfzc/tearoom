// Tearoom room interface: DOM renderers for the room list, roster, and messages.
// No state lives here; app.js owns selection, polling, and sending.

export function element(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

// One stable hue per name: room avatars, sender bubble tints, and name
// labels all read from it, so a sender looks the same everywhere.
export function senderHue(name) {
  let hash = 0;
  for (const ch of name) hash = (hash * 31 + ch.codePointAt(0)) >>> 0;
  return hash % 360;
}

// A soft per-room gradient for the initial avatar, in the spirit of the
// contact avatars in the native Messages list.
export function avatarColor(name) {
  const hue = senderHue(name);
  return `linear-gradient(160deg, hsl(${hue} 28% 52%), hsl(${(hue + 40) % 360} 30% 38%))`;
}

export function avatarFill(node, name) {
  node.textContent = (name || "?").slice(0, 1).toUpperCase();
  node.style.background = name ? avatarColor(name) : "#8e8e93";
}

const MENTION = /@[\p{L}\p{N}_]+/gu;

export function roomTree(rooms) {
  const byParent = new Map();
  for (const room of rooms) {
    const siblings = byParent.get(room.parent) || [];
    siblings.push(room);
    byParent.set(room.parent, siblings);
  }
  const ordered = [];
  const visit = (parentId, depth) => {
    for (const room of byParent.get(parentId) || []) {
      ordered.push({ room, depth });
      visit(room.id, depth + 1);
    }
  };
  visit("", 0);
  return ordered;
}

export function previewText(room, depth) {
  const last = room.last && typeof room.last === "object" ? room.last : null;
  if (!last || typeof last.text !== "string" || !last.text) {
    return depth ? "" : room.description || "";
  }
  const author = typeof last.author === "string" ? last.author : "";
  return author ? `${author}: ${last.text}` : last.text;
}

function roomRow(room, depth, selected, onSelect) {
  const item = element("button", "room-item");
  item.style.paddingLeft = `${10 + depth * 12}px`;
  const avatar = element("span", "avatar");
  avatarFill(avatar, room.name);
  const text = element("span", "row-text");
  const top = element("span", "row-top");
  top.append(element("span", "row-name", room.name));
  const last = room.last && typeof room.last === "object" ? room.last : null;
  const time = last ? timeLabel(last.at) : "";
  if (time) top.append(element("time", "row-time", time));
  text.append(top, element("span", "row-preview", previewText(room, depth)));
  item.append(avatar, text);
  if (selected) item.classList.add("selected");
  item.title = room.description || room.name;
  item.addEventListener("click", () => onSelect(room));
  return item;
}

// The dashboard rail: one section per workspace, its room tree nested under
// a header that names the workspace directory.
export function renderDashboard(container, workspaces, selected, onSelect) {
  container.replaceChildren(
    ...workspaces.map((workspace) => {
      const section = element("section", "ws");
      const head = element("button", "ws-head");
      head.title = workspace.root;
      head.append(element("span", "ws-name", workspace.name || workspace.root));
      const count = Array.isArray(workspace.rooms) ? workspace.rooms.length : 0;
      head.append(element("span", "ws-count", `${count}`));
      head.addEventListener("click", () => {
        const first = (workspace.rooms || [])[0];
        if (first) onSelect(workspace.root, first);
      });
      section.append(head);
      for (const { room, depth } of roomTree(workspace.rooms || [])) {
        const isSelected =
          selected && selected.root === workspace.root && selected.id === room.id;
        const item = roomRow(room, depth, isSelected, (room) =>
          onSelect(workspace.root, room),
        );
        section.append(item);
      }
      return section;
    }),
  );
}

export function renderRoster(node, room, onRole) {
  const roles = Array.isArray(room?.roles) ? room.roles : [];
  if (!roles.length) {
    node.textContent = "";
    node.hidden = true;
    return;
  }
  node.hidden = false;
  const list = element("div", "roster-list");
  node.replaceChildren(list);
  roles.forEach((role) => {
    const card = element(onRole ? "button" : "span", onRole ? "roster-card" : "roster-card static");
    const avatar = element("span", "role-avatar");
    avatar.setAttribute("aria-hidden", "true");
    avatarFill(avatar, role);
    const detail = element("span", "role-detail");
    detail.append(element("span", "role-name", `@${role}`));
    if (onRole) {
      card.type = "button";
      card.setAttribute("aria-label", `Open @${role}'s terminal. Checking status.`);
      card.addEventListener("click", () => onRole(role));
      const positionDetails = () => {
        const left = Math.min(card.offsetLeft, node.clientWidth - detail.offsetWidth);
        detail.style.left = `${Math.max(0, left)}px`;
      };
      card.addEventListener("pointerenter", positionDetails);
      card.addEventListener("focus", positionDetails);
      card.addEventListener("keydown", (event) => {
        if (event.key === "Escape") card.dataset.dismissed = "";
      });
      card.addEventListener("blur", () => delete card.dataset.dismissed);
      card.addEventListener("pointerleave", () => delete card.dataset.dismissed);
      const status = element("span", "role-status", "Checking…");
      status.dataset.role = role;
      detail.append(status);
    }
    card.append(avatar, detail);
    list.append(card);
  });
}

export function roleStatus(info) {
  if (!info) return "Status unavailable";
  if (!info.live) return "No active panel";
  const labels = {
    working: "Working", idle: "Idle", done: "Turn ended", blocked: "Needs attention",
    error: "Needs attention", unreported: "No status reported",
  };
  return Object.hasOwn(labels, info.status) ? labels[info.status] : "Status unknown";
}

const checkingStatus = new WeakSet();
export function refreshRoster(node, { root, id }, isCurrent) {
  node.querySelectorAll(".role-status").forEach(async (status) => {
    if (checkingStatus.has(status)) return;
    checkingStatus.add(status);
    const info = await window.__TAURI__.core.invoke("panel_info", {
      root, room: id, role: status.dataset.role,
    }).catch(() => null);
    checkingStatus.delete(status);
    if (!status.isConnected || !isCurrent()) return;
    const label = roleStatus(info);
    if (status.textContent !== label) {
      status.textContent = label;
      status.closest("button").setAttribute("aria-label", `Open @${status.dataset.role}'s terminal. ${label}.`);
    }
    status.dataset.state = info ? (info.live ? info.status : "missing") : "unavailable";
  });
}

function withMentions(text) {
  const frag = document.createDocumentFragment();
  let last = 0;
  for (const match of text.matchAll(MENTION)) {
    if (match.index > last) {
      frag.appendChild(document.createTextNode(text.slice(last, match.index)));
    }
    frag.appendChild(element("span", "mention", match[0]));
    last = match.index + match[0].length;
  }
  if (last < text.length) frag.appendChild(document.createTextNode(text.slice(last)));
  return frag;
}

function contentOf(record) {
  const content = record.content;
  if (typeof content === "string") {
    return { text: content, mentions: [], replyTo: null, kind: "text" };
  }
  if (content && typeof content === "object" && typeof content.text === "string") {
    return {
      text: content.text,
      mentions: Array.isArray(content.mentions) ? content.mentions : [],
      replyTo: content.reply_to || null,
      kind: "text",
    };
  }
  return { text: JSON.stringify(content), mentions: [], replyTo: null, kind: "data" };
}

function timeLabel(at) {
  const date = new Date(at);
  return Number.isNaN(date.getTime())
    ? ""
    : date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

export class MessageList {
  // Bubbles in the native-Messages shape: a small name label above the first
  // bubble of each run from one author; the person's own messages carry no
  // label and sit on the right. `isRole` marks senders whose live panel can
  // be opened; `onRole` receives their clicks.
  constructor(list, statusNode, isRole = null, onRole = null) {
    this.list = list;
    this.statusNode = statusNode;
    this.isRole = isRole;
    this.onRole = onRole;
    this.count = 0;
    this.lastSeq = 0;
    this.lastKey = null;
  }

  reset() {
    this.list.replaceChildren();
    this.count = 0;
    this.lastSeq = 0;
    this.lastKey = null;
  }

  status(text) {
    this.statusNode.textContent = text;
  }

  showEmpty() {
    this.list.replaceChildren(element("p", "empty-line", "No messages yet."));
  }

  append(record, me) {
    if (this.count === 0) this.list.replaceChildren();
    const info = contentOf(record);
    const mine = record.author === me;
    const sameRun = record.author === this.lastKey;
    this.lastKey = record.author;
    const node = element("li", `msg${mine ? " mine" : ""}`);
    node.style.setProperty("--sender-h", String(senderHue(record.author || "?")));
    if (sameRun) node.classList.add("run");
    if (!mine && !sameRun) {
      const meta = element("div", "meta");
      const author = element("span", "author", record.author || "(unnamed)");
      if (this.isRole?.(record.author)) {
        author.classList.add("link");
        author.title = `Open the @${record.author} panel`;
        author.addEventListener("click", () => this.onRole?.(record.author));
      }
      meta.append(author);
      const time = timeLabel(record.at);
      if (time) meta.append(` · ${time}`);
      node.append(meta);
    }
    const bubble = element("div", info.kind === "data" ? "bubble data" : "bubble");
    if (info.replyTo) {
      bubble.append(
        element("span", "reply-ref", `↩ ${info.replyTo.room}#${info.replyTo.seq}`),
        document.createTextNode(" "),
      );
    }
    if (info.kind === "text") bubble.append(withMentions(info.text));
    else bubble.textContent = info.text;
    node.append(bubble);
    if (info.mentions.length) {
      const routes = element("p", "routes", "→ ");
      info.mentions.forEach((name, index) => {
        if (index) routes.append(", ");
        routes.append(element("span", "route", `@${name}`));
      });
      node.append(routes);
    }
    if (this.isRole?.(record.author)) {
      const jump = element("button", "panel-jump", "▸");
      jump.title = `Open @${record.author}'s live panel`;
      jump.setAttribute("aria-label", jump.title);
      jump.addEventListener("click", () => this.onRole?.(record.author));
      node.append(jump);
    }
    this.list.append(node);
    this.count += 1;
    this.lastSeq = Math.max(this.lastSeq, record.seq);
  }
}
