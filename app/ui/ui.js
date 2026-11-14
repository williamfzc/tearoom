// Tearoom desktop face: DOM renderers for the conversation list and bubbles.
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

export function renderRooms(container, rooms, selectedId, onSelect) {
  container.replaceChildren(
    ...roomTree(rooms).map(({ room, depth }) => {
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
      if (room.id === selectedId) item.classList.add("selected");
      item.title = room.description || room.name;
      item.addEventListener("click", () => onSelect(room.id));
      return item;
    }),
  );
}

export function renderRoster(node, room) {
  const roles = Array.isArray(room?.roles) ? room.roles : [];
  if (!roles.length) {
    node.textContent = "";
    node.hidden = true;
    return;
  }
  node.hidden = false;
  node.replaceChildren("with ");
  roles.forEach((role, index) => {
    if (index) node.append(", ");
    node.append(element("span", "roster-role", `@${role}`));
  });
}

export function renderAddresses(container, room, selected) {
  const roles = Array.isArray(room?.roles) ? room.roles : [];
  container.replaceChildren(
    ...roles.map((role) => {
      const chip = element("label", "address-chip");
      const input = document.createElement("input");
      input.type = "checkbox";
      input.name = "address";
      input.value = role;
      input.checked = selected.has(role);
      chip.append(input, element("span", null, `@${role}`));
      return chip;
    }),
  );
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
  // label and sit on the right.
  constructor(list, statusNode) {
    this.list = list;
    this.statusNode = statusNode;
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
      meta.append(element("span", "author", record.author || "(unnamed)"));
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
    this.list.append(node);
    this.count += 1;
    this.lastSeq = Math.max(this.lastSeq, record.seq);
  }
}
