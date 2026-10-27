// Pure helpers for scripts/ui-compare.mjs (docs/ui-guidelines.md, Visual comparison). They do
// no I/O apart from the `exists` callback, so Jest can test them without a browser.

import path from "node:path";

/** The two viewports every comparison uses. */
export const viewports = [
  { name: "desktop", width: 1280, height: 900 },
  { name: "mobile", width: 390, height: 844 },
];

/** A channel difference above this counts the pixel as different (0–255). */
export const channelThreshold = 16;

export const usage = "Usage: npm run ui:compare -- <route> <design-file>   (e.g. -- /login Auth)";

/**
 * parseArgs reads the route and the design file name from the command line arguments.
 * @param {string[]} argv the arguments after the script name
 * @returns {{ route: string, design: string }}
 */
export function parseArgs(argv) {
  const args = argv.filter((a) => a !== "--");
  if (args.length !== 2) {
    throw new Error(usage);
  }
  const [rawRoute, rawDesign] = args;
  const route = rawRoute.startsWith("/") ? rawRoute : `/${rawRoute}`;
  const design = path.basename(rawDesign).replace(/\.html$/i, "").replace(/_m$/, "");
  if (!design) {
    throw new Error(usage);
  }
  return { route, design };
}

/**
 * designFiles returns the design file for each viewport: <Name>.html on the desktop and
 * <Name>_m.html on the phone, or <Name>.html when there is no mobile artboard.
 * @param {string} dir the design/screens folder
 * @param {string} design the design name without extension
 * @param {(file: string) => boolean} exists
 * @returns {{ desktop: string, mobile: string }}
 */
export function designFiles(dir, design, exists) {
  const desktop = path.join(dir, `${design}.html`);
  if (!exists(desktop)) {
    throw new Error(`No design file ${desktop}`);
  }
  const mobile = path.join(dir, `${design}_m.html`);
  return { desktop, mobile: exists(mobile) ? mobile : desktop };
}

/**
 * slug turns a route into a file name part: "/" is "root", "/profile/abc" is "profile-abc".
 * @param {string} route
 */
export function slug(route) {
  const s = route
    .replace(/[?#].*$/, "")
    .replace(/[^A-Za-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return s || "root";
}

/**
 * diffPercent compares two RGBA buffers of the same size and returns the share of pixels,
 * in percent, where any colour channel differs by more than the threshold.
 * @param {ArrayLike<number>} a
 * @param {ArrayLike<number>} b
 * @param {number} [threshold]
 */
export function diffPercent(a, b, threshold = channelThreshold) {
  if (a.length !== b.length || a.length % 4 !== 0) {
    throw new Error("Images differ in size");
  }
  const pixels = a.length / 4;
  if (pixels === 0) {
    return 0;
  }
  let different = 0;
  for (let i = 0; i < a.length; i += 4) {
    if (
      Math.abs(a[i] - b[i]) > threshold ||
      Math.abs(a[i + 1] - b[i + 1]) > threshold ||
      Math.abs(a[i + 2] - b[i + 2]) > threshold
    ) {
      different++;
    }
  }
  return (different / pixels) * 100;
}
