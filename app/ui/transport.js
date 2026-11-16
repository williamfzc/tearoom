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
    capabilities: { terminal: true, pick: true },
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
      throw new Error(
        "Cannot reach the Mac. Check that both devices are on the same network and the service is running.",
      );
    }
    const body = response.status === 204 ? {} : await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(body.error || `Request failed (${response.status})`);
    return body;
  }

  // A send that fails to reach the Mac may or may not have landed; the same
  // key and content replay safely, so the pending key survives until a send
  // is confirmed or refused.
  let pendingKey = null;

  return {
    kind: "remote",
    capabilities: { terminal: false, pick: false },
    connected: () => Boolean(saved()),
    savedAddress: () => saved()?.baseUrl || "",
    async pair(address, code) {
      const baseUrl = address.trim().replace(/\/+$/, "");
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
      pendingKey = null;
      return body;
    },
    disconnect() {
      localStorage.removeItem(REMOTE_KEY);
      pendingKey = null;
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
      if (!pendingKey) pendingKey = crypto.randomUUID();
      try {
        const result = await call("/v1/send", {
          method: "POST",
          body: JSON.stringify({
            room,
            text,
            to,
            author,
            key: pendingKey,
          }),
        });
        pendingKey = null;
        return { record: result.record, warning: null };
      } catch (error) {
        if (!error.message.startsWith("Cannot reach")) pendingKey = null;
        throw error;
      }
    },
  };
}

export const transport = isRemote() ? remote() : desktop();
