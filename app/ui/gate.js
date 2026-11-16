// The boot gates: what a person sees before any data flows. The desktop
// picks a workspace folder when discovery finds nothing; the phone pairs
// with the Mac using the address and code printed by `tearoom collab pair`.
// The app controller supplies the hooks that run once a gate is passed.

export function initGates(els, transport, hooks) {
  const showGate = (message) => {
    els.gateError.textContent = message || "";
    els.gateError.hidden = !message;
    els.pick.hidden = transport.kind !== "desktop";
    els.gateHint.textContent = message
      ? "Tearoom could not discover workspaces."
      : transport.kind === "remote"
        ? "Your Mac's workspace has no rooms yet. Create one with tearoom room init."
        : "No Tearoom workspaces found on this machine yet. Point at one to get started.";
    els.app.hidden = true;
    els.gate.hidden = false;
  };

  const showConnect = () => {
    els.app.hidden = true;
    els.gate.hidden = true;
    els.connect.hidden = false;
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

  return { showGate, showConnect };
}
