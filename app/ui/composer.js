// The composer owns draft text and explicit Room-role selection at the UI boundary.
// It sends both through the desktop or LAN transport.

import { transport } from "./transport.js";

export class Composer {
  constructor(els) {
    this.els = els;
    this.sending = false;
    this.roles = [];
    this.recipients = new Set();
    this.roomVersion = 0;
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

  setRoom(room, reset = false) {
    this.roles = Array.isArray(room?.roles) ? room.roles : [];
    if (reset) {
      this.roomVersion += 1;
      this.recipients.clear();
      this.dismissed = null;
      this.#closeSuggestions();
      this.error("");
    } else {
      for (const role of this.recipients) {
        if (!this.roles.includes(role)) this.recipients.delete(role);
      }
      this.#updateSuggestions();
    }
    this.#renderRecipients();
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
    const roomVersion = this.roomVersion;
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
      if (this.roomVersion === roomVersion) {
        if (this.els.text.value === text) this.els.text.value = "";
        for (const role of to) this.recipients.delete(role);
        this.#renderRecipients();
        this.#resize();
      }
      await loadHistory();
      if (result.warning) {
        if (this.roomVersion === roomVersion) {
          this.error(`Message saved, but collaboration did not start: ${result.warning}`, true);
        }
        return "warning";
      }
      return "sent";
    } catch (error) {
      if (this.roomVersion === roomVersion) this.error(String(error));
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
