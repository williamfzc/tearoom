// The role-panel drawer: an inline terminal attached to a role's hc panel.
// The stream is herdr-core's framed terminal.attach — first paint, then live
// bytes; typing goes to the agent's PTY exactly as it would from a Herdr pane.

import { avatarFill, roleStatus } from "./ui.js";

// The Tauri core is resolved only when a panel is actually attached: the
// phone face imports this module but has no terminal commands, so it must
// not need window.__TAURI__ to boot.
function core() {
  const api = window.__TAURI__?.core;
  if (!api) throw new Error("Terminal panels need the desktop app.");
  return api;
}
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
    this.generation = 0;
    this.timer = null;
    this.attaching = Promise.resolve();
    this.previousFocus = null;
    this.els.close.addEventListener("click", () => this.close());
    window.addEventListener("resize", () => this.#fit());
    window.addEventListener("focus", () => this.#refreshVisible());
    document.addEventListener("visibilitychange", () => this.#refreshVisible());
    this.#wireResize();
  }

  // Drag the drawer's left edge to resize; the width is remembered.
  #wireResize() {
    const { drawer, resize: handle } = this.els;
    let startX = 0;
    let startWidth = 0;
    let railWidth = 0;
    let moved = false;
    const onMove = (event) => {
      if (!handle.offsetWidth || (!moved && event.clientX === startX)) return;
      moved = true;
      const requested = startWidth + startX - event.clientX;
      const midpoint = (drawer.parentElement.clientWidth - railWidth) / 2;
      const offset = requested - midpoint;
      // Hold the equal split within 5px and ease back to direct tracking at 20px.
      const release = Math.min(1, Math.max(0, (Math.abs(offset) - 5) / 15));
      const width = `${Math.round(midpoint + offset * release)}px`;
      if (drawer.style.getPropertyValue("--drawer-width") !== width) {
        this.onLayoutChange?.();
        drawer.style.setProperty("--drawer-width", width);
        this.#fit();
      }
      // Only the indicator gives when the panes hit a layout limit.
      const resisted = drawer.getBoundingClientRect().width - requested;
      handle.style.setProperty("--resize-offset", `${5 * Math.tanh(resisted / 24)}px`);
    };
    const onUp = () => {
      handle.classList.remove("dragging");
      handle.style.removeProperty("--resize-offset");
      document.removeEventListener("mousemove", onMove);
      document.removeEventListener("mouseup", onUp);
      if (!moved || !handle.offsetWidth) return;
      const width = `${Math.round(drawer.getBoundingClientRect().width)}px`;
      drawer.style.setProperty("--drawer-width", width);
      try {
        localStorage.setItem("tearoom.drawerWidth", width);
      } catch {
        /* Storage may be refused; the width just is not remembered. */
      }
    };
    handle.addEventListener("mousedown", (event) => {
      event.preventDefault();
      moved = false;
      startX = event.clientX;
      startWidth = drawer.getBoundingClientRect().width;
      railWidth = parseFloat(getComputedStyle(drawer).getPropertyValue("--rail-width"));
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

  #feedback(text) {
    if (this.els.feedback.textContent === text) return;
    this.els.feedback.textContent = text;
    this.els.feedback.hidden = !text;
  }

  async show(root, room, role) {
    this.close();
    const generation = this.generation;
    this.previousFocus = document.activeElement;
    this.onLayoutChange?.();
    this.#renderChrome(role);
    try {
      const saved = localStorage.getItem("tearoom.drawerWidth");
      if (/^\d+(\.\d+)?px$/.test(saved)) {
        this.els.drawer.style.setProperty("--drawer-width", saved);
      }
    } catch {
      /* Storage may be refused; the default width applies. */
    }
    this.els.drawer.hidden = false;
    this.els.close.focus({ preventScroll: true });
    this.status("connecting…");
    this.#feedback("Checking for a live terminal…");
    const refresh = async () => {
      try {
        const info = await core().invoke("panel_info", { root, room, role });
        if (generation !== this.generation) return;
        this.status(roleStatus(info));
        if (info.live && !this.term) {
          this.#feedback("Waiting for terminal output…");
          const attaching = this.#start(root, room, role, generation);
          this.attaching = attaching.catch(() => {
            if (generation === this.generation) this.#feedback("Could not connect to the terminal.");
          });
          await this.attaching;
        } else if (!info.live && !this.term) {
          this.#feedback("Use @ in the Room to request work from this role.");
        }
      } catch {
        if (generation === this.generation) {
          this.status("Status unavailable");
          if (!this.term) this.#feedback("Could not check for a live terminal.");
        }
      }
      if (generation === this.generation) this.timer = setTimeout(refresh, 2500);
    };
    await refresh();
  }

  async #start(root, room, role, generation) {
    await this.attaching;
    if (generation !== this.generation) return;
    const theme = window.matchMedia("(prefers-color-scheme: light)").matches
      ? LIGHT
      : DARK;
    this.term = new window.Terminal({ theme, fontFamily: "ui-monospace, Menlo, monospace", fontSize: 12 });
    this.fit = new window.FitAddon.FitAddon();
    this.term.loadAddon(this.fit);
    this.term.open(this.els.term);
    this.term.onRender(() => {
      if (generation !== this.generation || this.els.feedback.hidden) return;
      const view = this.term.buffer.active;
      for (let row = 0; row < this.term.rows; row += 1) {
        if (view.getLine(view.viewportY + row)?.translateToString(true).trim()) {
          this.#feedback("");
          return;
        }
      }
    });
    this.fit.fit();

    const channel = new (core().Channel)();
    channel.onmessage = (message) => {
      if (generation !== this.generation) return;
      if (message.t === "data") {
        const raw = atob(message.d);
        const bytes = new Uint8Array(raw.length);
        for (let i = 0; i < raw.length; i += 1) bytes[i] = raw.charCodeAt(i);
        this.term?.write(bytes);
      } else if (message.t === "close") {
        this.session = null;
        this.term?.dispose();
        this.term = null;
        this.els.term.replaceChildren();
        this.status(message.reason ? `panel ended: ${message.reason}` : "detached");
        this.#feedback("The terminal disconnected.");
      }
    };
    let result;
    try {
      result = await core().invoke("attach_panel", {
        root,
        room,
        role,
        cols: this.term.cols,
        rows: this.term.rows,
        on: channel,
      });
    } catch (error) {
      if (generation === this.generation) {
        this.term?.dispose();
        this.term = null;
        this.els.term.replaceChildren();
      }
      throw error;
    }
    if (generation !== this.generation || !this.term) {
      await core().invoke("detach_panel", { session: result.session }).catch(() => {});
      return;
    }
    this.session = result.session;

    this.term.onData((data) => {
      if (this.session == null) return;
      core().invoke("terminal_write", { session: this.session, data: toBase64(data) }).catch(() => {});
    });
    this.#syncSize();
    if (document.activeElement === this.els.close) this.term.focus();
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

  #refreshVisible() {
    if (document.hidden || !this.open || !this.term) return;
    this.#fit();
    this.term.refresh(0, this.term.rows - 1);
  }

  // Forwards the viewer size to the core, which relayouts the panel's PTY.
  #syncSize() {
    if (this.session == null || !this.term) return;
    core().invoke("terminal_resize", {
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
    this.generation += 1;
    clearTimeout(this.timer);
    if (this.open) this.onLayoutChange?.();
    const restoreFocus = this.els.drawer.contains(document.activeElement);
    if (this.session != null) {
      const detached = core().invoke("detach_panel", { session: this.session }).catch(() => {});
      this.attaching = Promise.all([this.attaching, detached]);
    }
    this.session = null;
    this.term?.dispose();
    this.term = null;
    this.els.term.replaceChildren();
    this.els.drawer.hidden = true;
    if (restoreFocus) this.previousFocus?.focus({ preventScroll: true });
    this.previousFocus = null;
  }
}
