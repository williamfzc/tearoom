// The composer: recipient chips, the text field, and the send flow.
// Sends go through the tearoom CLI via the send_message command; a saved
// record whose collaboration failed to start comes back as a warning.

import { renderAddresses } from "./ui.js";

export class Composer {
  constructor(els) {
    this.els = els;
    this.sending = false;
    this.#wire();
  }

  get author() {
    return this.els.author.value.trim();
  }

  setAuthor(value) {
    if (!this.els.author.value && value) this.els.author.value = value;
  }

  error(text, warn = false) {
    const node = this.els.error;
    node.textContent = text || "";
    node.hidden = !text;
    node.classList.toggle("warn", warn);
  }

  // Renders the recipient chips for a room, keeping whatever was checked.
  // Switching rooms collapses the recipient panel.
  setRoom(room) {
    const selected = new Set(
      Array.from(
        this.els.addresses.querySelectorAll('input[name="address"]:checked'),
        (input) => input.value,
      ),
    );
    renderAddresses(this.els.addresses, room, selected);
    this.els.recipientToggle.hidden = !(room?.roles || []).length;
    this.els.recipients.hidden = true;
    this.els.recipientToggle.setAttribute("aria-expanded", "false");
    this.#updateRecipients();
    this.error("");
  }

  focus() {
    this.els.text.focus();
  }

  async send(roomId, loadHistory) {
    if (!roomId || this.sending) return;
    const text = this.els.text.value;
    if (!text.trim()) {
      this.error("Write a message first.");
      this.els.text.focus();
      return;
    }
    const to = Array.from(
      this.els.addresses.querySelectorAll('input[name="address"]:checked'),
      (input) => input.value,
    );
    this.sending = true;
    this.#updateSendState();
    this.error("");
    try {
      const result = await window.__TAURI__.core.invoke("send_message", {
        room: roomId,
        text,
        to,
        author: this.author,
      });
      this.els.text.value = "";
      this.#resize();
      await loadHistory();
      if (result.warning) {
        this.error(`Message saved, but collaboration did not start: ${result.warning}`, true);
        return "warning";
      }
      return "sent";
    } catch (error) {
      this.error(String(error));
      return "failed";
    } finally {
      this.sending = false;
      this.#updateSendState();
    }
  }

  #updateRecipients() {
    const chosen = Array.from(
      this.els.addresses.querySelectorAll('input[name="address"]:checked'),
      (input) => input.value,
    );
    this.els.recipientToggle.textContent = chosen.length
      ? `To · @${chosen[0]}${chosen.length > 1 ? ` +${chosen.length - 1}` : ""}`
      : "To";
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
    this.els.composer.addEventListener("submit", (event) => {
      event.preventDefault();
      // The send flow itself is owned by the app controller.
      this.els.composer.dispatchEvent(
        new CustomEvent("tearoom:send", { bubbles: true }),
      );
    });
    this.els.recipientToggle.addEventListener("click", () => {
      this.els.recipients.hidden = !this.els.recipients.hidden;
      this.els.recipientToggle.setAttribute(
        "aria-expanded",
        String(!this.els.recipients.hidden),
      );
    });
    this.els.addresses.addEventListener("change", () => this.#updateRecipients());
    this.els.author.addEventListener("input", () => {
      try {
        localStorage.setItem("tearoom.author", this.els.author.value);
      } catch {
        /* Storage may be refused; the field still works. */
      }
    });
    this.els.text.addEventListener("input", () => {
      this.#resize();
      this.#updateSendState();
    });
    this.els.text.addEventListener("keydown", (event) => {
      if (event.key === "Enter" && !event.shiftKey && !event.isComposing) {
        event.preventDefault();
        if (!this.els.send.disabled) this.els.composer.requestSubmit();
      }
    });
    try {
      this.els.author.value = localStorage.getItem("tearoom.author") || "";
    } catch {
      this.els.author.value = "";
    }
    this.#updateSendState();
  }
}
