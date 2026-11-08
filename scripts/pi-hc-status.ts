// Reports Pi's lifecycle to hc; task results remain Room records.
export default function (pi: any) {
  const report = (state: string) => process.stdout.write(`\u001b]7501;state=${state}\u0007`);
  pi.on("session_start", () => report("idle"));
  pi.on("agent_start", () => report("working"));
  pi.on("agent_end", () => report("done"));
  pi.on("agent_error", () => report("error"));
}
