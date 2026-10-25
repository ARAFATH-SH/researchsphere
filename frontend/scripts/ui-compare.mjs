// ui:compare (dev only): screenshots an app route and a design/screens file at 1280x900 and
// 390x844, saves app | design | diff side by side in ui-compare/ and prints the share of
// pixels that differ. See docs/ui-guidelines.md, Visual comparison.
//
//   npm run ui:compare -- <route> <design-file>
//
// Settings (all optional): UI_COMPARE_BASE_URL (default http://localhost:3000),
// UI_COMPARE_EMAIL and UI_COMPARE_PASSWORD (sign in first, for signed-in routes),
// UI_COMPARE_CHROME (browser executable; default the installed Google Chrome),
// UI_COMPARE_DESIGN_DIR (default ../design/screens).

import { existsSync, mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import { chromium } from "playwright-core";

import { channelThreshold, designFiles, diffPercent, parseArgs, slug, viewports } from "./ui-compare-lib.mjs";

const frontendDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const designDir = path.resolve(frontendDir, process.env.UI_COMPARE_DESIGN_DIR ?? "../design/screens");
const outDir = path.join(frontendDir, "ui-compare");
const baseUrl = (process.env.UI_COMPARE_BASE_URL ?? "http://localhost:3000").replace(/\/+$/, "");

async function screenshot(context, url) {
  const page = await context.newPage();
  try {
    await page.goto(url, { waitUntil: "networkidle" });
    await page.evaluate(() => document.fonts.ready);
    return await page.screenshot();
  } finally {
    await page.close();
  }
}

// compose draws the two screenshots and a diff mask (changed pixels red over a faded copy of
// the app) side by side in the browser, and returns the PNG plus both images' RGBA bytes.
async function compose(context, app, design, threshold) {
  const page = await context.newPage();
  try {
    return await page.evaluate(
      async ({ app, design, threshold }) => {
        const load = async (b64) => createImageBitmap(await (await fetch(`data:image/png;base64,${b64}`)).blob());
        const [a, d] = await Promise.all([load(app), load(design)]);
        const w = a.width;
        const h = a.height;
        const pixels = (img) => {
          const c = new OffscreenCanvas(w, h);
          const ctx = c.getContext("2d");
          ctx.drawImage(img, 0, 0);
          return ctx.getImageData(0, 0, w, h);
        };
        const pa = pixels(a);
        const pd = pixels(d);
        const mask = new ImageData(w, h);
        for (let i = 0; i < pa.data.length; i += 4) {
          const changed =
            Math.abs(pa.data[i] - pd.data[i]) > threshold ||
            Math.abs(pa.data[i + 1] - pd.data[i + 1]) > threshold ||
            Math.abs(pa.data[i + 2] - pd.data[i + 2]) > threshold;
          if (changed) {
            mask.data.set([220, 38, 38, 255], i);
          } else {
            const grey = 255 - (255 - (pa.data[i] + pa.data[i + 1] + pa.data[i + 2]) / 3) * 0.2;
            mask.data.set([grey, grey, grey, 255], i);
          }
        }
        const gap = 16;
        const out = new OffscreenCanvas(w * 3 + gap * 2, h);
        const ctx = out.getContext("2d");
        ctx.fillStyle = "#808080";
        ctx.fillRect(0, 0, out.width, out.height);
        ctx.drawImage(a, 0, 0);
        ctx.drawImage(d, w + gap, 0);
        ctx.putImageData(mask, (w + gap) * 2, 0);
        const toB64 = async (blob) => {
          const bytes = new Uint8Array(await blob.arrayBuffer());
          let s = "";
          for (let i = 0; i < bytes.length; i += 0x8000) {
            s += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
          }
          return btoa(s);
        };
        return {
          png: await toB64(await out.convertToBlob({ type: "image/png" })),
          app: await toB64(new Blob([pa.data])),
          design: await toB64(new Blob([pd.data])),
        };
      },
      { app: app.toString("base64"), design: design.toString("base64"), threshold },
    );
  } finally {
    await page.close();
  }
}

async function main() {
  const { route, design } = parseArgs(process.argv.slice(2));
  const files = designFiles(designDir, design, existsSync);
  mkdirSync(outDir, { recursive: true });

  const executablePath = process.env.UI_COMPARE_CHROME;
  const browser = await chromium.launch(executablePath ? { executablePath } : { channel: "chrome" });
  try {
    for (const vp of viewports) {
      const context = await browser.newContext({ viewport: { width: vp.width, height: vp.height } });
      try {
        const email = process.env.UI_COMPARE_EMAIL;
        if (email) {
          const res = await context.request.post(`${baseUrl}/api/v1/auth/login`, {
            data: { email, password: process.env.UI_COMPARE_PASSWORD ?? "" },
          });
          if (!res.ok()) {
            throw new Error(`Sign-in failed: HTTP ${res.status()}`);
          }
        }
        const app = await screenshot(context, `${baseUrl}${route}`);
        const designFile = files[vp.name];
        const designShot = await screenshot(context, pathToFileURL(designFile).href);
        const result = await compose(context, app, designShot, channelThreshold);

        const out = path.join(outDir, `${slug(route)}__${design}__${vp.name}.png`);
        writeFileSync(out, Buffer.from(result.png, "base64"));
        const pct = diffPercent(Buffer.from(result.app, "base64"), Buffer.from(result.design, "base64"));
        console.log(
          `${vp.name.padEnd(7)} ${vp.width}x${vp.height}  ${route} vs ${path.basename(designFile)}  diff ${pct.toFixed(2)}%  -> ${path.relative(frontendDir, out)}`,
        );
      } finally {
        await context.close();
      }
    }
  } finally {
    await browser.close();
  }
}

main().catch((err) => {
  console.error(err instanceof Error ? err.message : err);
  process.exit(1);
});
