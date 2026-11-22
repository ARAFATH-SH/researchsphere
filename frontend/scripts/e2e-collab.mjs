// End-to-end test of real-time editing (slice 22, docs/sync.md; FR-12.4, FR-12.5, FR-12.7,
// NFR-2) in real browsers against the running dev servers: four editors type in main.tex at
// the same time while a viewer watches; every text must end up the same, the viewer must not
// be able to type, everyone sees the others' cursors and who is editing, and the sync server
// stores the result. It also measures how long one editor's keystroke takes to appear for the
// others (NFR-2: under 1 second).
//
//   make up && make migrate && make seed-dev
//   make api && make sync && make web      (in their own terminals)
//   cd frontend && npm run e2e:collab
//
// It signs in as the seed-dev accounts test1@example.com (owner) to test4@example.com
// (editors) and admin@example.com (viewer) with DEV_SEED_PASSWORD (from the environment, or
// ../.env), creates a throwaway project, invites the others, and deletes the project at the
// end, also when a check fails. E2E_BASE_URL (default http://localhost:3000) and E2E_CHROME
// (a Chrome binary; default the installed Chrome) are optional. Exits 1 on the first failed check.

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { chromium } from "playwright-core";

const frontendDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const baseUrl = (process.env.E2E_BASE_URL ?? "http://localhost:3000").replace(/\/+$/, "");

function seedPassword() {
  if (process.env.DEV_SEED_PASSWORD) return process.env.DEV_SEED_PASSWORD;
  try {
    const env = readFileSync(path.resolve(frontendDir, "../.env"), "utf8");
    const line = env.split("\n").find((l) => l.startsWith("DEV_SEED_PASSWORD="));
    return line?.slice("DEV_SEED_PASSWORD=".length).trim().replace(/^["']|["']$/g, "") || undefined;
  } catch {
    return undefined;
  }
}

const editors = ["test1@example.com", "test2@example.com", "test3@example.com", "test4@example.com"];
const viewerEmail = "admin@example.com";

function step(text) {
  console.log(`- ${text}`);
}

async function api(context, method, route, data) {
  const res = await context.request.fetch(`${baseUrl}/api/v1${route}`, { method, data });
  const body = res.status() === 204 ? null : await res.json().catch(() => null);
  return { status: res.status(), body };
}

// The editor's text without the other members' cursor labels (widgets inside the lines).
function editorText(page) {
  return page.evaluate(() => {
    const content = document.querySelector(".cm-content");
    if (!content) return null;
    return [...content.querySelectorAll(".cm-line")]
      .map((line) => {
        const copy = line.cloneNode(true);
        copy.querySelectorAll(".cm-rs-caret").forEach((c) => c.remove());
        return copy.textContent.replace(/⁠/g, "");
      })
      .join("\n");
  });
}

async function waitFor(check, what, timeoutMs = 10_000) {
  const end = Date.now() + timeoutMs;
  let last;
  while (Date.now() < end) {
    last = await check();
    if (last === true) return;
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(`timed out waiting for ${what}${last && last !== false ? `: ${last}` : ""}`);
}

const percentile = (sorted, p) => sorted[Math.min(sorted.length - 1, Math.ceil((p / 100) * sorted.length) - 1)];

async function main() {
  const password = seedPassword();
  assert.ok(password, "set DEV_SEED_PASSWORD (make seed-dev uses it), or put it in ../.env");
  const executablePath = process.env.E2E_CHROME;
  const browser = await chromium.launch(executablePath ? { executablePath } : { channel: "chrome" });
  const people = [];
  let owner;
  let projectId;
  let title;
  try {
    for (const email of [...editors, viewerEmail]) {
      const context = await browser.newContext();
      const login = await api(context, "POST", "/auth/login", { email, password });
      assert.equal(login.status, 200, `sign-in as ${email}: HTTP ${login.status} (run make seed-dev)`);
      const me = await api(context, "GET", "/auth/me");
      people.push({ email, context, id: me.body.id, viewer: email === viewerEmail });
    }
    owner = people[0];
    step("signed in as test1-4 (editors) and admin (viewer)");

    title = `Live editing e2e ${Date.now().toString(36)}`;
    const created = await api(owner.context, "POST", "/projects", { title, description: "Throwaway project of npm run e2e:collab." });
    assert.equal(created.status, 201, `create project: HTTP ${created.status} ${JSON.stringify(created.body)}`);
    projectId = created.body.id;
    for (const p of people.slice(1)) {
      const inv = await api(owner.context, "POST", `/projects/${projectId}/invitations`, { userId: p.id, role: p.viewer ? "viewer" : "editor" });
      assert.equal(inv.status, 201, `invite ${p.email}: HTTP ${inv.status} ${JSON.stringify(inv.body)}`);
      const acc = await api(p.context, "POST", `/projects/${projectId}/invitations/${inv.body.id}/accept`);
      assert.ok(acc.status < 300, `accept as ${p.email}: HTTP ${acc.status} ${JSON.stringify(acc.body)}`);
    }
    const tree = await api(owner.context, "GET", `/projects/${projectId}/files`);
    const mainFile = tree.body.items.find((f) => f.path === tree.body.mainFilePath);
    assert.ok(mainFile, "the project has no main.tex");
    step(`project ${projectId} with 4 editors and 1 viewer`);

    const problems = [];
    await Promise.all(
      people.map(async (p) => {
        p.page = await p.context.newPage();
        p.page.on("pageerror", (err) => problems.push(`${p.email}: ${err.message}`));
        await p.page.goto(`${baseUrl}/projects/${projectId}/workspace`);
        const label = p.viewer ? "main.tex, view only" : "Editing main.tex";
        p.editor = p.page.getByRole("textbox", { name: label });
        await p.editor.waitFor({ timeout: 30_000 });
        if (!p.viewer) await p.page.getByRole("banner").getByRole("status").getByText("Live", { exact: true }).waitFor({ timeout: 15_000 });
      }),
    );
    step("all five have main.tex open; the editors are Live");

    // Who is editing: the four editors, never the viewer (FR-12.5).
    await owner.page.getByRole("group", { name: "4 people editing this file" }).waitFor({ timeout: 10_000 });
    step('the bar says "4 / 4 editing"');

    // Four editors type at once, each at the end of their own line.
    const lines = 3;
    await Promise.all(
      people
        .filter((p) => !p.viewer)
        .map(async (p, i) => {
          await p.editor.click();
          await p.page.keyboard.press("Control+End");
          for (let n = 1; n <= lines; n++) {
            await p.page.keyboard.press("Enter");
            await p.page.keyboard.type(`% editor ${i + 1} line ${n}`, { delay: 15 });
          }
        }),
    );
    step(`four editors typed ${lines} lines each at the same time`);

    // The viewer tries to type: nothing changes anywhere (FR-12.7).
    const viewer = people.find((p) => p.viewer);
    assert.equal(await viewer.editor.getAttribute("contenteditable"), "false");
    await viewer.editor.click();
    await viewer.page.keyboard.type("VIEWER WAS HERE");

    let texts = [];
    await waitFor(async () => {
      texts = await Promise.all(people.map((p) => editorText(p.page)));
      return texts.every((t) => t === texts[0]) || `texts differ (${texts.map((t) => t?.length).join(", ")} characters)`;
    }, "every text to match");
    for (let i = 1; i <= 4; i++) for (let n = 1; n <= lines; n++) assert.ok(texts[0].includes(`% editor ${i} line ${n}`), `editor ${i} line ${n} is missing`);
    assert.ok(!texts[0].includes("VIEWER"), "the viewer's typing got in");
    step(`all five texts match (${texts[0].length} characters), with every editor's lines and nothing from the viewer`);

    // Everyone sees the others' cursors with names.
    const labels = await owner.page.locator(".cm-rs-caret-label").allTextContents();
    assert.ok(labels.length >= 3, `the owner sees ${labels.length} other cursors`);
    step(`the owner sees ${labels.length} other cursors: ${labels.join(", ")}`);

    // NFR-2: one keystroke to the other editors, 20 times.
    const writer = people[1];
    const readers = people.filter((p) => !p.viewer && p !== writer);
    const delays = [];
    for (let k = 0; k < 20; k++) {
      const marker = `#${k}#`;
      const typed = Date.now();
      await writer.page.keyboard.type(marker);
      await Promise.all(
        readers.map(async (r) => {
          await r.page.waitForFunction((m) => document.querySelector(".cm-content")?.textContent?.includes(m), marker, { timeout: 5000 });
          delays.push(Date.now() - typed);
        }),
      );
    }
    delays.sort((a, b) => a - b);
    step(`edit to other editors: p50 ${percentile(delays, 50)} ms, p95 ${percentile(delays, 95)} ms, max ${delays.at(-1)} ms (${delays.length} samples)`);
    assert.ok(percentile(delays, 95) < 1000, "NFR-2: p95 over 1 second");

    // The sync server stores it within 5 s (NFR-10.1) and the bar says Saved.
    const final = await editorText(owner.page);
    await waitFor(async () => {
      const f = await api(owner.context, "GET", `/projects/${projectId}/files/${mainFile.id}`);
      return f.body?.content === final || "not stored yet";
    }, "the stored text", 10_000);
    await owner.page.getByRole("banner").getByRole("status").getByText("Saved", { exact: true }).waitFor({ timeout: 10_000 });
    step("the server stored the same text, and the bar says Saved");
    assert.deepEqual(problems, [], "page errors");
    console.log("e2e live editing: all checks passed");
  } finally {
    if (projectId && owner) {
      const del = await api(owner.context, "DELETE", `/projects/${projectId}?confirm=${encodeURIComponent(title)}`).catch(() => null);
      console.log(del?.status === 204 ? `- deleted project ${projectId}` : `- could not delete project ${projectId} (HTTP ${del?.status})`);
    }
    await browser.close();
  }
}

main().catch((err) => {
  console.error(`e2e live editing failed: ${err.message}`);
  process.exit(1);
});
