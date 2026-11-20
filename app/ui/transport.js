// The transport seam: how this UI reaches Tearoom. The desktop face talks to
// the Tauri commands (Store reads in-process, sends through the CLI); the
// remote face talks to the LAN service on the paired Mac over fetch
// (docs/reference/lan-face.md). Both return the same payload shapes, so the
// renderers stay face-agnostic.

const REMOTE_KEY = "tearoom.remote";

function isRemote() {
  try {
    if (localStorage.getItem(REMOTE_KEY)) return true;
  } catch {
    /* storage refused; fall through to the platform check */
  }
  return /iPhone|iPad|iPod/i.test(navigator.userAgent);
}

function desktop() {
  const { invoke } = window.__TAURI__.core;
  return {
    kind: "desktop",
    capabilities: { terminal: true, pick: true, remove: true },
    async listWorkspaces() {
      return invoke("workspaces");
    },
    async pickWorkspace() {
      return invoke("workspace_pick");
    },
    async records(root, room, after) {
      return invoke("room_records", { root, room, after });
    },
    async send({ root, room, text, to, author }) {
      return invoke("send_message", { root, room, text, to, author });
    },
    async deleteRoom({ root, room, confirm }) {
      return invoke("delete_room", { root, room, confirm });
    },
  };
}

function remote() {
  const saved = () => {
    try {
      return JSON.parse(localStorage.getItem(REMOTE_KEY) || "null");
    } catch {
      return null;
    }
  };

  async function call(path, options = {}) {
    const state = saved();
    if (!state) throw new Error("Not paired to a Mac yet.");
    let response;
    try {
      response = await fetch(`${state.baseUrl}${path}`, {
        ...options,
        headers: {
          ...(options.body ? { "content-type": "application/json" } : {}),
          ...(state.token ? { authorization: `Bearer ${state.token}` } : {}),
        },
      });
    } catch {
      const error = new Error(
        "Cannot reach the Mac. Check that both devices are on the same network and the service is running.",
      );
      // The request left the device; it may or may not have landed.
      error.uncertain = true;
      throw error;
    }
    const body = response.status === 204 ? {} : await response.json().catch(() => ({}));
    if (!response.ok) {
      const error = new Error(body.error || `Request failed (${response.status})`);
      // A 5xx may hide a saved record or a failed response write. Refusals
      // (4xx) are definite, so their key can be forgotten.
      error.uncertain = response.status >= 500;
      throw error;
    }
    return body;
  }

  // A send that fails after the request left the device may or may not have
  // landed. Remember one key per exact payload across reloads: the same
  // payload replays the original record, while changed text, room, or
  // recipients get a fresh key instead of a false idempotency conflict.
  // Unconfirmed keys are never silently evicted; a payload is forgotten only
  // when its send is confirmed or definitely refused. If storage refuses a
  // write, the in-memory map still covers this session.
  // randomUUID needs a secure context, which a webview's custom scheme may
  // not provide — getRandomValues always exists.
  const PENDING = "tearoom.pendingSends";
  const newKey = () =>
    crypto.randomUUID
      ? crypto.randomUUID()
      : Array.from(
          crypto.getRandomValues(new Uint8Array(16)),
          (byte) => byte.toString(16).padStart(2, "0"),
        ).join("");
  // The outgoing payload is normalized once: recipient order is canonical
  // because the Store fingerprints mention order, and the author is trimmed
  // exactly as the CLI does. Identity and body therefore always agree.
  const payloadOf = ({ room, text, to, author }) => ({
    room,
    text,
    to: Array.isArray(to) ? [...to].sort() : [],
    author: String(author ?? "").trim(),
  });
  const identityOf = (payload) =>
    JSON.stringify([payload.room, payload.text, payload.to, payload.author]);
  const loadPending = () => {
    try {
      const value = JSON.parse(localStorage.getItem(PENDING) || "{}");
      return value && typeof value === "object" && !Array.isArray(value) ? value : {};
    } catch {
      return {};
    }
  };
  let pending = loadPending();
  const savePending = () => {
    try {
      localStorage.setItem(PENDING, JSON.stringify(pending));
    } catch {
      /* Storage may be refused; the in-memory map still covers this session. */
    }
  };
  const pendingKey = (identity) => {
    const saved = pending[identity];
    const savedKey = typeof saved === "string" ? saved : saved?.key;
    if (typeof savedKey === "string" && savedKey) {
      pending[identity] = savedKey;
      return savedKey;
    }
    const key = newKey();
    pending[identity] = key;
    savePending();
    return key;
  };
  const settlePending = (identity, key) => {
    const saved = pending[identity];
    const savedKey = typeof saved === "string" ? saved : saved?.key;
    if (savedKey !== key) return;
    delete pending[identity];
    savePending();
  };
  const resetPending = () => {
    pending = {};
    savePending();
  };

  return {
    kind: "remote",
    capabilities: { terminal: false, pick: false },
    connected: () => Boolean(saved()),
    savedAddress: () => saved()?.baseUrl || "",
    async pair(address, code) {
      // `collab pair` prints the address with its /v1 prefix; accept either
      // shape and store the bare origin — every call appends /v1 itself.
      const baseUrl = address
        .trim()
        .replace(/\/+$/, "")
        .replace(/\/v1$/, "");
      const response = await fetch(`${baseUrl}/v1/pair`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ code: code.trim() }),
      }).catch(() => {
        throw new Error("Cannot reach that address. Check the network and the printed URL.");
      });
      const body = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(body.error || `Pairing failed (${response.status})`);
      localStorage.setItem(
        REMOTE_KEY,
        JSON.stringify({ baseUrl, token: body.token, workspace: body.workspace }),
      );
      // Keys belong to the previous Mac; a new pairing starts clean.
      resetPending();
      return body;
    },
    disconnect() {
      localStorage.removeItem(REMOTE_KEY);
      resetPending();
    },
    async listWorkspaces() {
      const state = saved();
      const { rooms } = await call("/v1/rooms");
      return {
        actor: "",
        workspaces: [
          { root: state.workspace, name: state.workspace, rooms: rooms || [] },
        ],
      };
    },
    async records(_root, room, after) {
      return call(`/v1/records?room=${encodeURIComponent(room)}&after=${after}`);
    },
    async send({ room, text, to, author }) {
      const payload = payloadOf({ room, text, to, author });
      const identity = identityOf(payload);
      const key = pendingKey(identity);
      try {
        const result = await call("/v1/send", {
          method: "POST",
          body: JSON.stringify({ ...payload, key }),
        });
        settlePending(identity, key);
        // A saved record whose wake failed comes back as a warning; the
        // composer shows it exactly as the desktop face does.
        return { record: result.record, warning: result.warning ?? null };
      } catch (error) {
        // A definite refusal did not save; an uncertain failure keeps its
        // key so retrying the same payload can only replay that record.
        if (!error.uncertain) settlePending(identity, key);
        throw error;
      }
    },
  };
}

export const transport = isRemote() ? remote() : desktop();
