/**
 * @jest-environment node
 */
import { readFileSync } from "node:fs";
import path from "node:path";

// The design tokens (--rs-*) in globals.css, their Tailwind mappings and the table in
// docs/ui-guidelines.md must stay in step.
const css = readFileSync(path.join(__dirname, "globals.css"), "utf8");
const guide = readFileSync(path.join(__dirname, "../../../docs/ui-guidelines.md"), "utf8");

const block = (start: string) => {
  const from = css.indexOf(start);
  expect(from).toBeGreaterThanOrEqual(0);
  return css.slice(from, css.indexOf("\n}", from));
};

const defined = new Map(
  [...block("/* Design tokens taken from design/screens/").matchAll(/^\s+(--rs-[a-z0-9-]+):\s*([^;]+);/gm)].map(
    (m) => [m[1], m[2].trim()],
  ),
);
const mappings = [...block("@theme inline {").matchAll(/^\s+(--[a-z0-9-]+):\s*var\((--rs-[a-z0-9-]+)\);/gm)].map(
  (m) => ({ tailwind: m[1], token: m[2] }),
);

it("defines the design tokens", () => {
  expect(defined.get("--rs-accent")).toBe("#0b7a75");
  expect(defined.get("--rs-accent-strong")).toBe("#0f766e");
  expect(defined.get("--rs-heading")).toBe("#0b5f58");
  expect(defined.get("--rs-border")).toBe("#dde3e1");
  expect(defined.get("--rs-content")).toBe("1128px");
  expect(defined.get("--rs-gutter")).toBe("16px");
  expect(defined.get("--rs-font-sans")).toMatch(/^var\(--font-source-sans\), "Source Sans 3"/);
});

it("maps every token to Tailwind and every mapping to a token", () => {
  const mapped = new Set(mappings.map((m) => m.token));
  expect([...defined.keys()].filter((t) => !mapped.has(t))).toEqual([]);
  expect(mappings.filter((m) => !defined.has(m.token))).toEqual([]);
});

it("documents every token in docs/ui-guidelines.md", () => {
  expect([...defined.keys()].filter((t) => !guide.includes(`\`${t}\``))).toEqual([]);
});

it("keeps the old colors until pages are restyled", () => {
  expect(css).toContain("--color-background: #f5f6f4;");
});

it("sets the page text in Source Sans 3 at 14px, line height 1.45 (UI.2)", () => {
  const body = css.slice(css.indexOf("\nbody {"));
  expect(body).toContain("font-family: var(--rs-font-sans);");
  expect(body).toContain("font-size: var(--rs-size-body);");
  expect(body).toContain("line-height: var(--rs-leading-body);");
  expect(body).toContain("color: var(--rs-text);");
  expect(css).not.toMatch(/--container-(marketing|page)|--font-sans:/);
});
