// Destructive Room removal at the app boundary: a two-step confirm whose
// guards live entirely in the tearoom CLI, like sends. The first click runs
// the CLI's guards unarmed and reads its confirmation sentence (the record
// count); the second deletes. The webview confirm replaces the CLI's
// --confirm repeat, never its guards, so both clicks re-check the world.
// Desktop only — the LAN face carries no destructive endpoint.

const DISARM_AFTER_MS = 8000;

export function initRoomDelete(button, transport, { status, onDeleted }) {
  if (!transport.capabilities.remove) {
    button.remove();
    return { target() {} };
  }
  let target = null;
  let armed = null;
  const render = () => {
    button.hidden = !target;
    button.textContent = armed
      ? `Confirm — ${armed.count} record${armed.count === "1" ? "" : "s"}`
      : "Delete";
    button.classList.toggle("armed", Boolean(armed));
    button.title = armed ? armed.reason : "Delete this room and its history";
    button.setAttribute("aria-label", armed
      ? "Confirm deleting this room and its history"
      : "Delete this room and its history");
  };
  const disarm = () => {
    if (armed) clearTimeout(armed.timer);
    armed = null;
    render();
  };
  const remove = async () => {
    const { root, id } = target;
    try {
      await transport.deleteRoom({ root, room: id, confirm: Boolean(armed) });
      disarm();
      onDeleted();
    } catch (error) {
      const match = String(error).match(/removes its (\d+) record/);
      if (armed || !match) {
        disarm();
        status(String(error));
        return;
      }
      // Guards passed; the CLI is asking for --confirm and told us the cost.
      armed = {
        count: match[1],
        reason: String(error),
        timer: setTimeout(disarm, DISARM_AFTER_MS),
      };
      render();
    }
  };
  button.addEventListener("click", () => {
    if (target) remove();
  });
  button.addEventListener("keydown", (event) => {
    if (event.key === "Escape") disarm();
  });
  button.addEventListener("blur", disarm);
  render();
  return {
    target(next) {
      target = next;
      disarm();
    },
  };
}
