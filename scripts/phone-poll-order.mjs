// Manual regression check for the phone face's overlapping-poll contract:
// an older listWorkspaces response must never overwrite newer state. It
// serves the production app/ui unmodified and injects only at the fetch
// boundary. Hand-run via `make phone-poll`; it needs Playwright and a
// Chromium build, so it is deliberately not part of `make check`.

import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { globSync } from "node:fs";
import { homedir, platform } from "node:os";
import { dirname, extname, join, normalize, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const uiArg = process.argv[2];
const UI = resolve(uiArg || join(here, "..", "app", "ui"));
const MIME = { ".html": "text/html; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".css": "text/css; charset=utf-8" };
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// Playwright is not a repository dependency. Accept an explicit module path,
// a normal resolution, or the npx cache this workspace has used for probes.
async function loadChromium() {
  const candidates = [process.env.PLAYWRIGHT, "playwright"];
  for (const home of [homedir()]) {
    candidates.push(...globSync(`${home}/.npm/_npx/*/node_modules/playwright/index.mjs`).sort().reverse());
  }
  for (const candidate of candidates.filter(Boolean)) {
    try {
      return (await import(candidate)).chromium;
    } catch {
      // try the next candidate
    }
  }
  console.error("phone-poll: Playwright not found. Set PLAYWRIGHT=/path/to/playwright/index.mjs or install it.");
  process.exit(2);
}

// The cached Chromium builds may not match the resolved Playwright version,
// so prefer an explicit browser and otherwise point at an installed build.
function findChrome() {
  if (process.env.CHROME) return process.env.CHROME;
  const suffixes = platform() === "darwin"
    ? ["chromium-*/chrome-mac*/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing"]
    : ["chromium-*/chrome-linux/chrome", "chromium-*/chrome-linux64/chrome"];
  const caches = platform() === "darwin"
    ? [`${homedir()}/Library/Caches/ms-playwright`]
    : [`${homedir()}/.cache/ms-playwright`, `${homedir()}/.cache/ms-playwright-go`];
  for (const cache of caches) {
    for (const suffix of suffixes) {
      const hits = globSync(`${cache}/${suffix}`).sort().reverse();
      if (hits.length) return hits[0];
    }
  }
  return undefined;
}

const chromium = await loadChromium();
const executablePath = findChrome();

const server = createServer(async (req, res) => {
  let path = normalize(decodeURIComponent(new URL(req.url, "http://127.0.0.1").pathname));
  if (path === "/") path = "/index.html";
  const file = join(UI, path);
  if (!resolve(file).startsWith(UI)) {
    res.writeHead(403).end("forbidden");
    return;
  }
  try {
    const body = await readFile(file);
    res.writeHead(200, { "content-type": MIME[extname(file)] || "application/octet-stream" });
    res.end(body);
  } catch {
    res.writeHead(404).end("not found");
  }
});
await new Promise((r) => server.listen(0, "127.0.0.1", r));
const BASE = `http://127.0.0.1:${server.address().port}`;

const rooms = [{ id: "main", name: "Main", parent: "", description: "", created: "2026-01-01T00:00:00Z", roles: ["worker"], last: null }];
const results = [];
const fail = (name, detail = "") => results.push({ ok: false, name, detail });
const pass = (name, detail = "") => results.push({ ok: true, name, detail });
const check = (name, ok, detail = "") => {
  (ok ? pass : fail)(name, detail);
  console.log(`${ok ? "PASS" : "FAIL"} ${name}${detail ? ` — ${detail}` : ""}`);
};

// Each scenario gets a fresh page so its call plan and interval are isolated.
async function scenario(name, { keepInterval }, body) {
  const browser = await chromium.launch({ executablePath, headless: true });
  const context = await browser.newContext({
    userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1",
    viewport: { width: 390, height: 844 },
  });
  await context.addInitScript(({ base, keep }) => {
    localStorage.setItem("tearoom.remote", JSON.stringify({ baseUrl: base, token: "tok", workspace: "demo" }));
    localStorage.setItem("tearoom.pendingSends", "{}");
    if (!keep) {
      const native = window.setInterval;
      window.setInterval = (fn, ms, ...args) => (ms === 2500 ? 0 : native(fn, ms, ...args));
    }
  }, { base: BASE, keep: keepInterval });
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(String(e)));
  const calls = [];
  let plan = [];
  await page.route("**/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/v1/rooms") {
      calls.push(calls.length + 1);
      const step = plan.shift() || { status: 200 };
      if (step.delay) await sleep(step.delay);
      if (step.fail) return route.abort("failed");
      return route.fulfill({ status: step.status, contentType: "application/json", body: JSON.stringify(step.body || { rooms }) });
    }
    if (path === "/v1/records") {
      return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ records: [] }) });
    }
    return route.fulfill({ status: 404, contentType: "application/json", body: JSON.stringify({ error: "unknown route" }) });
  });
  const visible = (sel) => page.locator(sel).isVisible();
  const fire = async (count) => {
    await page.evaluate((n) => {
      for (let i = 0; i < n; i += 1) document.dispatchEvent(new Event("visibilitychange"));
    }, count);
  };
  try {
    await page.goto(BASE);
    await page.locator("#app").waitFor({ state: "visible", timeout: 5000 });
    await page.waitForTimeout(350);
    check(`${name}: baseline`, (await visible("#app")) && (await page.textContent("#room-name")) === "Main");
    await body({ page, calls, visible, fire, arm: (steps) => { plan = steps; } });
    check(`${name}: no page errors`, errors.length === 0, errors.join(" | "));
  } catch (error) {
    check(`${name}: completed`, false, String(error?.stack || error));
  } finally {
    await browser.close();
  }
}

console.log(`phone-poll: ui=${UI} chrome=${executablePath || "playwright default"}\n`);

// 1. Older request fails late; newer request succeeds fast. The newer success
//    is the current state, so the stale failure must not hide the app.
await scenario("stale failure after newer success", { keepInterval: false }, async ({ calls, visible, fire, arm }) => {
  arm([{ delay: 1200, fail: true }, { delay: 60, status: 200 }]);
  await fire(2);
  await sleep(1500);
  check(
    "overlapping poll: a stale failed response does not hide the app after a newer success",
    (await visible("#app")) && !(await visible("#gate")),
    `app=${await visible("#app")} gate=${await visible("#gate")} calls=${calls.length}`,
  );
});

// 2. Reverse the order: older success arrives late, newer request fails fast.
//    The newer failure's gate must not be cleared by the stale success.
await scenario("stale success after newer failure", { keepInterval: false }, async ({ calls, visible, fire, arm }) => {
  arm([{ delay: 1200, status: 200 }, { delay: 60, status: 500, body: { error: "local failure" } }]);
  await fire(2);
  await sleep(400);
  const gateAfterFreshFailure = await visible("#gate");
  await sleep(1200);
  check(
    "overlapping poll: a stale successful response does not clear a newer failure gate",
    gateAfterFreshFailure && (await visible("#gate")) && !(await visible("#app")),
    `gate@400ms=${gateAfterFreshFailure} gate@1600ms=${await visible("#gate")} app=${await visible("#app")} calls=${calls.length}`,
  );
});

// 3. Production interval kept: one visibility poll fails 3.7 s late while the
//    2500 ms interval poll succeeds first. The older failure must not hide the
//    newer state.
await scenario("visibility poll plus production interval", { keepInterval: true }, async ({ calls, visible, fire, arm }) => {
  arm([{ delay: 3700, fail: true }]);
  await fire(1);
  await sleep(4400);
  check(
    "visibility poll + interval: the older failed poll does not hide the newer success",
    (await visible("#app")) && !(await visible("#gate")),
    `app=${await visible("#app")} gate=${await visible("#gate")} calls=${calls.length}`,
  );
});

server.close();
const failed = results.filter((r) => !r.ok);
console.log(`\n${results.length - failed.length}/${results.length} passed; ${failed.length} failed`);
process.exit(failed.length ? 1 : 0);
