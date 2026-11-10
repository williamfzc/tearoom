// Local browser UI rendering: room tree, message groups, and recipient chips.
// Every record field reaches the DOM through textContent, never as markup.

export const element = (tag, className, text) => {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
};

export function formatTime(value) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const now = new Date();
  const sameDay =
    date.getFullYear() === now.getFullYear() &&
    date.getMonth() === now.getMonth() &&
    date.getDate() === now.getDate();
  if (sameDay) {
    return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  }
  const day = date.toLocaleDateString([], {
    month: "short",
    day: "numeric",
    ...(date.getFullYear() === now.getFullYear() ? {} : { year: "numeric" }),
  });
  return `${day}, ${date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`;
}

function initials(author) {
  const name = (author || "").trim();
  if (!name) return "·";
  const parts = name.split(/\s+/).filter(Boolean);
  if (parts.length > 1) {
    return `${Array.from(parts[0])[0]}${Array.from(parts[1])[0]}`.toUpperCase();
  }
  return Array.from(name)[0].toUpperCase();
}

function messageOf(content) {
  if (typeof content === "string") {
    return { text: content, mentions: [], replyTo: null };
  }
  if (content && typeof content === "object" && typeof content.text === "string") {
    return {
      text: content.text,
      mentions: Array.isArray(content.mentions) ? content.mentions : [],
      replyTo: content.reply_to || null,
    };
  }
  return {
    text: JSON.stringify(content, null, 2),
    mentions: [],
    replyTo: null,
  };
}

function byName(a, b) {
  return a.name.localeCompare(b.name);
}

export function renderRooms(container, countElement, rooms, selected, onSelect) {
  container.replaceChildren();
  const byParent = new Map();
  for (const room of rooms) {
    const parent = room.parent || "";
    if (!byParent.has(parent)) byParent.set(parent, []);
    byParent.get(parent).push(room);
  }
  const known = new Set(rooms.map((room) => room.name));
  const roots = rooms
    .filter((room) => !room.parent || !known.has(room.parent))
    .sort(byName);
  const visited = new Set();

  function branch(items) {
    const list = document.createElement("ul");
    for (const room of items) {
      if (visited.has(room.name)) continue;
      visited.add(room.name);
      const item = document.createElement("li");
      const button = document.createElement("button");
      button.type = "button";
      button.className = "room-link";
      button.dataset.room = room.name;
      button.append(element("span", "name", room.name));
      const children = (byParent.get(room.name) || []).slice().sort(byName);
      if (children.length) {
        const count = element("span", "child-count", String(children.length));
        count.setAttribute("aria-hidden", "true");
        button.append(count);
        button.setAttribute(
          "aria-label",
          `${room.name}, ${children.length} child room${children.length === 1 ? "" : "s"}`,
        );
      }
      if (room.name === selected) {
        button.setAttribute("aria-current", "true");
      }
      button.addEventListener("click", () => onSelect(room.name));
      item.append(button);
      if (children.length) item.append(branch(children));
      list.append(item);
    }
    return list;
  }

  if (rooms.length) {
    container.append(branch(roots));
  } else {
    container.append(
      element(
        "p",
        "rail-empty",
        "No rooms yet. Create one with tearoom room init --name <name>.",
      ),
    );
  }
  countElement.textContent = String(rooms.length);
}

export function renderAddresses(addresses, recipients, room, selected = new Set()) {
  addresses.replaceChildren();
  const roles = room && Array.isArray(room.roles) ? room.roles : [];
  recipients.hidden = roles.length === 0;
  for (const role of roles) {
    const label = element("label", "recipient");
    const input = document.createElement("input");
    input.type = "checkbox";
    input.name = "address";
    input.value = role;
    input.checked = selected.has(role);
    label.append(input, element("span", "", `@${role}`));
    addresses.append(label);
  }
}

export class MessageView {
  constructor(records, statusElement) {
    this.records = records;
    this.statusElement = statusElement;
    this.reset();
  }

  reset() {
    this.records.replaceChildren();
    this.seen = new Set();
    this.lastSeq = 0;
    this.lastAuthor = null;
    this.lastAt = 0;
    this.statusElement.textContent = "";
  }

  get count() {
    return this.seen.size;
  }

  status(text) {
    this.statusElement.textContent = text;
  }

  showEmpty() {
    if (this.count || this.records.querySelector(".message.notice")) return;
    this.records.append(element("li", "message notice", "No messages yet."));
  }

  append(record) {
    if (!record || typeof record.seq !== "number" || this.seen.has(record.seq)) {
      return;
    }
    this.seen.add(record.seq);
    this.lastSeq = Math.max(this.lastSeq, record.seq);
    const notice = this.records.querySelector(".message.notice");
    if (notice) notice.remove();

    const at = Date.parse(record.at) || 0;
    const grouped =
      this.lastAuthor === (record.author || "") &&
      at > 0 &&
      this.lastAt > 0 &&
      Math.abs(at - this.lastAt) < 5 * 60 * 1000;
    this.lastAuthor = record.author || "";
    this.lastAt = at;

    const message = messageOf(record.content);
    const item = element("li", grouped ? "message grouped" : "message");
    const avatar = element("span", "avatar", initials(record.author));
    avatar.setAttribute("aria-hidden", "true");
    item.append(avatar);

    const main = element("div", "message-main");
    const meta = element("div", "message-meta");
    meta.append(
      element("span", "message-author", record.author || "unsigned"),
      element(
        "span",
        "message-coordinate",
        `#${record.seq} · ${formatTime(record.at)}`,
      ),
    );
    const bubble = element("div", "bubble");
    bubble.append(element("p", "message-text", message.text));
    if (message.mentions.length || message.replyTo) {
      const addresses = element("div", "message-addresses");
      for (const role of message.mentions) {
        addresses.append(element("span", "badge", `@${role}`));
      }
      if (message.replyTo && message.replyTo.room) {
        addresses.append(
          element(
            "span",
            "badge reply",
            `↳ ${message.replyTo.room}#${message.replyTo.seq}`,
          ),
        );
      }
      bubble.append(addresses);
    }
    main.append(meta, bubble);
    item.append(main);
    this.records.append(item);
  }
}
