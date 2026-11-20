// The boot gates: what a person sees before any data flows. The desktop
// picks a workspace folder when discovery finds nothing; the phone pairs
// with the Mac using the address and code printed by `tearoom collab pair`.
// The app controller supplies the hooks that run once a gate is passed.

export function initGates(els, transport, hooks) {
  const firstRoom = "tearoom room init --name project";
  const showGate = (message, hasWorkspace = false) => {
    els.gateError.textContent = message || "";
    els.gateError.hidden = !message;
    els.pick.hidden = transport.kind !== "desktop" || hasWorkspace;
    els.reconnect.hidden = transport.kind !== "remote";
    els.gateHint.textContent =
      transport.kind === "remote"
        ? message
          ? "The phone could not read the paired workspace. Tearoom keeps trying; check the Mac's service."
          : `Your Mac's workspace has no rooms yet. Create one with ${firstRoom}.`
        : message
          ? "Tearoom could not discover workspaces."
          : hasWorkspace
            ? `No rooms yet. In your workspace folder, run ${firstRoom}.`
            : `Choose an existing Tearoom workspace, or run ${firstRoom} in your project folder.`;
    els.app.hidden = true;
    els.gate.hidden = false;
  };

  const showConnect = () => {
    els.app.hidden = true;
    els.gate.hidden = true;
    els.reconnect.hidden = true;
    els.connect.hidden = false;
  };

  // One authority for "the face is usable": every gate path that succeeds
  // comes back through here, so a recovered poll cannot leave the app hidden.
  const showApp = () => {
    els.gate.hidden = true;
    els.connect.hidden = true;
    els.app.hidden = false;
  };

  els.pick.addEventListener("click", async () => {
    let result;
    try {
      result = await transport.pickWorkspace();
    } catch (error) {
      showGate(String(error));
      return;
    }
    if (!result.root) return; // the person cancelled the dialog
    hooks.afterPick();
  });

  // A revoked token or a changed Mac must not leave the phone on a dead gate:
  // forget the pairing and return to the address/code screen.
  els.reconnect.addEventListener("click", () => {
    transport.disconnect?.();
    showConnect();
  });

  if (transport.savedAddress) els.connectAddress.value = transport.savedAddress();
  els.connectForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    els.connectError.textContent = "";
    els.connectError.hidden = true;
    els.connectSubmit.disabled = true;
    try {
      await transport.pair(els.connectAddress.value, els.connectCode.value);
      hooks.afterConnect();
    } catch (error) {
      els.connectError.textContent = String(error);
      els.connectError.hidden = false;
    } finally {
      els.connectSubmit.disabled = false;
    }
  });

  return { showApp, showGate, showConnect };
}
