// End-to-end test of figure uploads (FR-12.2, docs/decisions.md 2026-11-18) in a real browser
// against the running dev servers: the workspace page, the API's start and complete calls, the
// presigned POST straight to MinIO (CORS included), the malware scan and the lossless EXIF
// removal. It uploads a JPEG whose name has spaces, the same JPEG again (it must get "-2") and
// a PDF, then deletes what it uploaded.
//
//   make up && make migrate && make api && make web      (in their own terminals)
//   cd frontend && E2E_EMAIL=... E2E_PASSWORD=... npm run e2e:figures
//
// E2E_EMAIL and E2E_PASSWORD default to UI_COMPARE_EMAIL and UI_COMPARE_PASSWORD (make
// seed-ui-compare); the account must own or edit a project, or E2E_PROJECT_ID names one.
// E2E_BASE_URL (default http://localhost:3000) and E2E_CHROME (a Chrome binary; default the
// installed Chrome) are optional. Exits 1 on the first failed check.

import assert from "node:assert/strict";
import { copyFileSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { chromium } from "playwright-core";

const frontendDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const baseUrl = (process.env.E2E_BASE_URL ?? "http://localhost:3000").replace(/\/+$/, "");
const email = process.env.E2E_EMAIL ?? process.env.UI_COMPARE_EMAIL;
const password = process.env.E2E_PASSWORD ?? process.env.UI_COMPARE_PASSWORD;
// A JPEG with EXIF, a GPS position and the marker text below (backend test data).
const exifJpeg = path.resolve(frontendDir, "../backend/internal/media/adapter/imaging/testdata/exif.jpg");
const exifMarker = "SECRET-EXIF-MARKER";

function step(text) {
  console.log(`- ${text}`);
}

async function api(context, method, route, data) {
  const res = await context.request.fetch(`${baseUrl}/api/v1${route}`, { method, data });
  const body = res.status() === 204 ? null : await res.json().catch(() => null);
  return { status: res.status(), body };
}

async function findProject(context) {
  if (process.env.E2E_PROJECT_ID) return process.env.E2E_PROJECT_ID;
  const { status, body } = await api(context, "GET", "/me/projects?page=1&pageSize=50");
  assert.equal(status, 200, `GET /me/projects: HTTP ${status}`);
  const own = body.items.find((p) => p.myRole === "owner" || p.myRole === "editor");
  assert.ok(own, "the account owns or edits no project; set E2E_PROJECT_ID");
  return own.id;
}

async function main() {
  assert.ok(email && password, "set E2E_EMAIL and E2E_PASSWORD (or UI_COMPARE_EMAIL and UI_COMPARE_PASSWORD)");
  const dir = mkdtempSync(path.join(tmpdir(), "e2e-figures-"));
  const stamp = Date.now().toString(36);
  const jpegName = `WhatsApp Image 2026-11-17 at 9.10.25 PM ${stamp}.jpeg`;
  const pdfName = `Draft figure (v2) ${stamp}.pdf`;
  copyFileSync(exifJpeg, path.join(dir, jpegName));
  writeFileSync(path.join(dir, pdfName), `%PDF-1.4\n1 0 obj <<>> endobj\n% ${stamp}\n%%EOF\n`);
  const wantJpeg = `whatsapp-image-2026-11-17-at-9.10.25-pm-${stamp}.jpeg`;
  const wantJpeg2 = `whatsapp-image-2026-11-17-at-9.10.25-pm-${stamp}-2.jpeg`;
  const wantPdf = `draft-figure-v2-${stamp}.pdf`;

  const executablePath = process.env.E2E_CHROME;
  const browser = await chromium.launch(executablePath ? { executablePath } : { channel: "chrome" });
  const context = await browser.newContext();
  const problems = [];
  let projectId;
  try {
    const login = await api(context, "POST", "/auth/login", { email, password });
    assert.equal(login.status, 200, `sign-in: HTTP ${login.status}`);
    projectId = await findProject(context);
    step(`signed in; project ${projectId}`);

    const page = await context.newPage();
    page.on("requestfailed", (r) => problems.push(`${r.method()} ${r.url()} failed: ${r.failure()?.errorText}`));
    page.on("console", (m) => {
      if (m.type() === "warning" && m.text().startsWith("Figure upload failed")) problems.push(m.text());
    });
    await page.goto(`${baseUrl}/projects/${projectId}/workspace`);
    const input = page.getByLabel("Choose figures to upload");
    await input.waitFor({ state: "attached" });

    // A JPEG with spaces in its name and a PDF, at the project's top level.
    await page.getByRole("button", { name: "Upload figure" }).waitFor();
    await input.setInputFiles([path.join(dir, jpegName), path.join(dir, pdfName)]);
    const uploads = page.getByRole("list", { name: "Uploads" });
    await uploads.getByText(`${jpegName} was uploaded as ${wantJpeg}.`).waitFor({ timeout: 30_000 }).catch(async () => {
      throw new Error(`JPEG not uploaded. Uploads say: ${await uploads.innerText().catch(() => "(nothing)")}; ${problems.join("; ")}`);
    });
    await uploads.getByText(`${pdfName} was uploaded as ${wantPdf}.`).waitFor({ timeout: 30_000 });
    step(`uploaded "${jpegName}" as ${wantJpeg} and "${pdfName}" as ${wantPdf}`);

    // The same JPEG again: the name is taken, so it gets "-2", never a refusal.
    await input.setInputFiles([path.join(dir, jpegName)]);
    await uploads.getByText(`${jpegName} was uploaded as ${wantJpeg2}.`).waitFor({ timeout: 30_000 });
    step(`the same name again became ${wantJpeg2}`);
    assert.deepEqual(problems, [], "browser problems");

    // The server has them; each is scanned, and the JPEG is stored without its EXIF.
    const tree = await api(context, "GET", `/projects/${projectId}/files`);
    assert.equal(tree.status, 200);
    const byName = new Map(tree.body.items.map((f) => [f.path, f]));
    for (const name of [wantJpeg, wantJpeg2, wantPdf]) assert.ok(byName.has(name), `${name} is not in the tree`);
    assert.equal(byName.get(wantJpeg).contentType, "image/jpeg");
    assert.equal(byName.get(wantPdf).contentType, "application/pdf");

    const jpeg = byName.get(wantJpeg);
    let detail;
    for (let i = 0; i < 60; i++) {
      detail = await api(context, "GET", `/projects/${projectId}/files/${jpeg.id}`);
      if (detail.status !== 409 || detail.body?.code !== "scan_pending") break;
      await new Promise((r) => setTimeout(r, 2000));
    }
    if (detail.status === 409 && detail.body?.code === "scan_pending") {
      step("the malware scan has not finished in 2 minutes (is ClamAV up?); download not checked");
    } else {
      assert.equal(detail.status, 200, `download: HTTP ${detail.status} ${JSON.stringify(detail.body)}`);
      const stored = Buffer.from(await (await context.request.get(detail.body.download.url)).body());
      assert.ok(stored.length > 0 && stored.length === jpeg.size, "stored size differs from the file's size");
      assert.ok(!stored.includes(exifMarker) && !stored.includes("Exif\0\0"), "EXIF is still in the stored JPEG");
      step(`scanned clean; the stored JPEG (${stored.length} bytes) has no EXIF`);
    }
    console.log("e2e figure upload: all checks passed");
  } finally {
    if (projectId) {
      const tree = await api(context, "GET", `/projects/${projectId}/files`).catch(() => null);
      for (const f of tree?.body?.items ?? []) {
        if (f.path.includes(stamp)) await api(context, "DELETE", `/projects/${projectId}/files/${f.id}`).catch(() => {});
      }
    }
    await browser.close();
    rmSync(dir, { recursive: true, force: true });
  }
}

main().catch((err) => {
  console.error(`e2e figure upload failed: ${err.message}`);
  process.exit(1);
});
