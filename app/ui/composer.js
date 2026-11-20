// The composer owns draft text and explicit Room-role selection at the UI boundary.
// It sends both through the desktop or LAN transport.

import { transport } from "./transport.js";

export class Composer {
  constructor(els) {
    this.els = els;
    this.sending = false;
    this.roles = [];
    this.recipients = new Set();
    this.drafts = new Map();
    this.draftKey = null;
    this.mention = null;
    this.dismissed = null;
    this.active = 0;
    this.composing = false;
    this.options = [];
    this.setAuthor();
    this.#wire();
  }

  setAuthor(value) {
    if (transport.kind === "remote") {
      try {
        value = localStorage.getItem("tearoom.author");
      } catch {
        value = "";
      }
    }
    this.author = value?.trim() || "me";
  }

  error(text, warn = false) {
    const node = this.els.error;
    node.textContent = text || "";
    node.hidden = !text;
    node.classList.toggle("warn", warn);
  }

  setRoom(target, room) {
    const key = target ? JSON.stringify([target.root, target.id]) : null;
    const field = this.els.text;
    this.roles = Array.isArray(room?.roles) ? room.roles : [];
    this.roomName = room?.name || "";
    if (key !== this.draftKey) {
      if (this.draftKey && (field.value || this.recipients.size)) {
        this.drafts.set(this.draftKey, { text: field.value, to: this.recipients });
      } else {
        this.drafts.delete(this.draftKey);
      }
      this.draftKey = key;
      const draft = this.drafts.get(key);
      field.value = draft?.text ?? "";
      this.recipients = draft?.to ?? new Set();
      this.dismissed = null;
      this.#closeSuggestions();
      this.error("");
    } else {
      this.#updateSuggestions();
    }
    this.#renderRecipients();
    this.#resize();
    this.#updateSendState();
  }

  // Roster-observed role states: addressed roles that are working make the
  // runtime queue the message, so the placeholder says so.
  setRoleStates(states) {
    this.roleStates = states;
    this.#renderRecipients();
  }

  clearDrafts() {
    this.setRoom(null);
    this.drafts = new Map();
  }

  focus() {
    this.els.text.focus();
  }

  async send(target, loadHistory) {
    if (!target || this.sending) return;
    const text = this.els.text.value;
    if (!text.trim()) {
      this.error("Write a message first.");
      this.focus();
      return;
    }
    const to = [...this.recipients];
    const { draftKey, drafts } = this;
    const stillInRoom = () => this.drafts === drafts && this.draftKey === draftKey;
    this.sending = true;
    this.#updateSendState();
    this.#closeSuggestions();
    this.error("");
    try {
      const result = await transport.send({
        root: target.root,
        room: target.id,
        text,
        to,
        author: this.author,
      });
      const draft = stillInRoom()
        ? { text: this.els.text.value, to: this.recipients }
        : drafts.get(draftKey);
      // An in-flight send may finish after the person has edited or switched drafts.
      if (draft?.text === text && draft.to.size === to.length && to.every((role) => draft.to.has(role))) {
        drafts.delete(draftKey);
        if (stillInRoom()) {
          this.els.text.value = "";
          this.recipients.clear();
        }
      }
      if (stillInRoom()) {
        this.#renderRecipients();
        this.#resize();
      }
      await loadHistory();
      if (result.warning) {
        if (stillInRoom()) {
          this.error(`Message saved, but collaboration did not start: ${result.warning}`, true);
        }
        return "warning";
      }
      return "sent";
    } catch (error) {
      if (stillInRoom()) this.error(String(error));
      return "failed";
    } finally {
      this.sending = false;
      this.#updateSendState();
    }
  }

  #renderRecipients() {
    const { selected } = this.els;
    selected.hidden = !this.recipients.size;
    selected.replaceChildren(...[...this.recipients].map((role) => {
      const chip = document.createElement("button");
      chip.type = "button";
      chip.className = "address-chip";
      chip.textContent = `@${role} ×`;
      chip.setAttribute("aria-label", `Remove @${role}`);
      chip.addEventListener("click", () => {
        this.recipients.delete(role);
        this.#renderRecipients();
        this.focus();
        this.#updateSuggestions();
      });
      return chip;
    }));
    const busy = [...this.recipients].filter((role) => this.roleStates?.get(role) === "working");
    this.els.text.placeholder = busy.length
      ? `Queue for ${busy.map((role) => `@${role}`).join(", ")} — working…`
      : `Message ${this.roomName}`.trimEnd();
  }

  #closeSuggestions() {
    this.mention = null;
    this.options = [];
    this.els.suggestions.hidden = true;
    this.els.suggestions.replaceChildren();
    this.els.text.removeAttribute("aria-activedescendant");
  }

  #updateSuggestions() {
    const { text, suggestions } = this.els;
    const caret = text.selectionStart;
    // Han text can start a mention without a space before the trigger.
    const match = text.value.slice(0, caret).match(
      /(?:^|[\s\p{Script=Han}，。；：、！？])[@＠]([\p{L}\p{Nd}][\p{L}\p{Nd}_-]*|)$/u,
    );
    if (this.composing || document.activeElement !== text || caret !== text.selectionEnd || !match) {
      this.#closeSuggestions();
      return;
    }
    const start = caret - match[1].length - 1;
    const key = `${start}:${match[1]}`;
    const prefix = match[1].toLowerCase();
    const options = this.roles.filter((role) =>
      !this.recipients.has(role) && role.toLowerCase().startsWith(prefix),
    );
    if (key === this.dismissed || !options.length) {
      this.#closeSuggestions();
      return;
    }
    if (key === this.mention?.key && options.length === this.options.length &&
        options.every((role, index) => role === this.options[index])) return;
    this.mention = { start, end: caret, key };
    this.options = options;
    this.active = 0;
    suggestions.replaceChildren(...options.map((role, index) => {
      const option = document.createElement("li");
      option.id = `${suggestions.id}-${index}`;
      option.className = "mention-option";
      option.setAttribute("role", "option");
      option.textContent = `@${role}`;
      option.addEventListener("pointerdown", (event) => event.preventDefault());
      option.addEventListener("click", () => this.#choose(role));
      return option;
    }));
    suggestions.hidden = false;
    this.#highlight();
  }

  #highlight() {
    const { suggestions, text } = this.els;
    for (const [index, option] of [...suggestions.children].entries()) {
      option.setAttribute("aria-selected", String(index === this.active));
    }
    const current = suggestions.children[this.active];
    text.setAttribute("aria-activedescendant", current.id);
    current.scrollIntoView({ block: "nearest" });
  }

  #choose(role) {
    const { text } = this.els;
    const { start, end } = this.mention;
    this.recipients.add(role);
    text.setRangeText("", start, end, "end");
    this.#closeSuggestions();
    this.#renderRecipients();
    this.#resize();
    this.#updateSendState();
    this.focus();
  }

  #updateSendState() {
    this.els.send.disabled =
      this.sending || this.els.text.value.trim().length === 0;
  }

  #resize() {
    this.els.text.style.height = "auto";
    this.els.text.style.height = `${Math.min(this.els.text.scrollHeight, 132)}px`;
  }

  #wire() {
    const { composer, text } = this.els;
    composer.addEventListener("submit", (event) => {
      event.preventDefault();
      composer.dispatchEvent(new CustomEvent("tearoom:send", { bubbles: true }));
    });
    text.addEventListener("input", (event) => {
      this.dismissed = null;
      this.#resize();
      this.#updateSendState();
      if (!event.isComposing) this.#updateSuggestions();
    });
    text.addEventListener("compositionstart", () => {
      this.composing = true;
      this.#closeSuggestions();
    });
    text.addEventListener("compositionend", () => {
      this.composing = false;
      this.#updateSuggestions();
    });
    text.addEventListener("blur", () => this.#closeSuggestions());
    text.addEventListener("click", () => this.#updateSuggestions());
    text.addEventListener("selectionchange", () => this.#updateSuggestions());
    text.addEventListener("keydown", (event) => {
      if (this.composing || event.isComposing || event.keyCode === 229) return;
      if (["ArrowUp", "ArrowDown", "Escape", "Enter", "Tab"].includes(event.key)) {
        this.#updateSuggestions();
      }
      if (this.mention) {
        switch (event.key) {
          case "ArrowUp":
          case "ArrowDown": {
            event.preventDefault();
            const step = event.key === "ArrowUp" ? -1 : 1;
            this.active = (this.active + step + this.options.length) % this.options.length;
            this.#highlight();
            return;
          }
          case "Escape":
            event.preventDefault();
            this.dismissed = this.mention.key;
            this.#closeSuggestions();
            return;
          case "Tab":
          case "Enter":
            if (event.shiftKey) break;
            event.preventDefault();
            this.#choose(this.options[this.active]);
            return;
        }
      }
      if (event.key === "Enter" && !event.shiftKey) {
        event.preventDefault();
        if (!this.els.send.disabled) composer.requestSubmit();
      }
    });
    this.#updateSendState();
  }
}
