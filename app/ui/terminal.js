// The role-panel drawer: an inline terminal attached to a role's hc panel.
// The stream is herdr-core's framed terminal.attach — first paint, then live
// bytes; typing goes to the agent's PTY exactly as it would from a Herdr pane.

import { avatarFill } from "./ui.js";

const { invoke, Channel } = window.__TAURI__.core;
const encoder = new TextEncoder();

const DARK = { background: "#0c0c0d", foreground: "#f5f5f7", cursor: "#f5f5f7" };
const LIGHT = { background: "#ffffff", foreground: "#111111", cursor: "#111111" };

function toBase64(text) {
  let string = "";
  for (const byte of encoder.encode(text)) string += String.fromCharCode(byte);
  return btoa(string);
}

export class TerminalDrawer {
  constructor(els) {
    this.els = els;
    this.onLayoutChange = els.onLayoutChange || null;
    this.session = null;
    this.term = null;
    this.fit = null;
    this.els.close.addEventListener("click", () => this.close());
    window.addEventListener("resize", () => this.#fit());
    this.#wireResize();
  }

  // Drag the drawer's left edge to resize; the width is remembered.
  #wireResize() {
    const handle = this.els.resize;
    let startX = 0;
    let startWidth = 0;
    const clamp = (width) =>
      // Leave the 300px rail plus a usable chat column.
      Math.max(320, Math.min(width, window.innerWidth - 680));
    const onMove = (event) => {
      const width = clamp(startWidth + (startX - event.clientX));
      this.els.drawer.style.width = `${width}px`;
      this.#fit();
    };
    const onUp = () => {
      handle.classList.remove("dragging");
      document.removeEventListener("mousemove", onMove);
      document.removeEventListener("mouseup", onUp);
      try {
        localStorage.setItem("tearoom.drawerWidth", this.els.drawer.style.width);
      } catch {
        /* Storage may be refused; the width just is not remembered. */
      }
    };
    handle.addEventListener("mousedown", (event) => {
      event.preventDefault();
      startX = event.clientX;
      startWidth = this.els.drawer.getBoundingClientRect().width;
      handle.classList.add("dragging");
      document.addEventListener("mousemove", onMove);
      document.addEventListener("mouseup", onUp);
    });
  }

  get open() {
    return !this.els.drawer.hidden;
  }

  status(text) {
    this.els.status.textContent = text || "";
  }

  async show(room, role) {
    this.onLayoutChange?.();
    this.#renderChrome(role);
    try {
      const saved = localStorage.getItem("tearoom.drawerWidth");
      if (saved) this.els.drawer.style.width = saved;
    } catch {
      /* Storage may be refused; the default width applies. */
    }
    this.els.drawer.hidden = false;
    this.status("connecting…");
    try {
      await this.#start(room, role);
    } catch (error) {
      this.status(String(error));
    }
  }

  async #start(room, role) {
    const info = await invoke("panel_info", { room, role });
    if (!info.live) {
      this.status("no live panel — wake the role with an addressed message");
      return;
    }
    this.status(info.status || "live");
    const theme = window.matchMedia("(prefers-color-scheme: light)").matches
      ? LIGHT
      : DARK;
    this.term = new window.Terminal({ theme, fontFamily: "ui-monospace, Menlo, monospace", fontSize: 12 });
    this.fit = new window.FitAddon.FitAddon();
    this.term.loadAddon(this.fit);
    this.term.open(this.els.term);
    this.fit.fit();

    const channel = new Channel();
    channel.onmessage = (message) => {
      if (message.t === "data") {
        const raw = atob(message.d);
        const bytes = new Uint8Array(raw.length);
        for (let i = 0; i < raw.length; i += 1) bytes[i] = raw.charCodeAt(i);
        this.term?.write(bytes);
      } else if (message.t === "close") {
        this.session = null;
        this.status(message.reason ? `panel ended: ${message.reason}` : "detached");
      }
    };
    const result = await invoke("attach_panel", {
      room,
      role,
      cols: this.term.cols,
      rows: this.term.rows,
      on: channel,
    });
    this.session = result.session;

    this.term.onData((data) => {
      if (this.session == null) return;
      invoke("terminal_write", { session: this.session, data: toBase64(data) }).catch(() => {});
    });
    this.#syncSize();
    this.term.focus();
    this.#fit();
  }

  #fit() {
    if (!this.open || !this.term || !this.fit) return;
    try {
      // Resizing snaps xterm's viewport to the bottom; a reader who scrolled
      // up gets their place back.
      const buffer = this.term.buffer.active;
      const viewportY = buffer.viewportY;
      const atBottom = viewportY >= buffer.length - this.term.rows;
      const before = `${this.term.cols}x${this.term.rows}`;
      this.fit.fit();
      if (`${this.term.cols}x${this.term.rows}` !== before) this.#syncSize();
      if (!atBottom && this.term.buffer.active.viewportY !== viewportY) {
        this.term.scrollToLine(
          Math.min(viewportY, this.term.buffer.active.length - 1),
        );
      }
    } catch {
      /* the container can be briefly unmeasured mid-transition */
    }
  }

  // Forwards the viewer size to the core, which relayouts the panel's PTY.
  #syncSize() {
    if (this.session == null || !this.term) return;
    invoke("terminal_resize", {
      session: this.session,
      cols: this.term.cols,
      rows: this.term.rows,
    }).catch(() => {});
  }

  #renderChrome(role) {
    this.els.name.textContent = `@${role}`;
    avatarFill(this.els.avatar, role);
  }

  close() {
    this.onLayoutChange?.();
    if (this.session != null) {
      invoke("detach_panel", { session: this.session }).catch(() => {});
    }
    this.session = null;
    this.term?.dispose();
    this.term = null;
    this.els.term.replaceChildren();
    this.els.drawer.hidden = true;
  }
}
