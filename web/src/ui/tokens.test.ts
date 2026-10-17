import fs from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

// Reads the exact token values from src/index.css and checks contrast in both palettes:
// 4.5:1 for text, 3:1 for UI (WCAG 1.4.3, 1.4.11) and for the focus indicator (2.4.13).
const css = fs.readFileSync(path.resolve(__dirname, "..", "index.css"), "utf8");

function palette(block: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const m of block.matchAll(/--([a-z-]+):\s*(#[0-9a-fA-F]{6})\s*;/g)) out[m[1]!] = m[2]!;
  return out;
}

const light = palette(
  css.slice(css.indexOf(":root {"), css.indexOf("@media (prefers-color-scheme: dark)")),
);
const darkStart = css.indexOf("@media (prefers-color-scheme: dark)");
const dark = { ...light, ...palette(css.slice(darkStart, css.indexOf("@theme inline"))) };

function lum(hex: string): number {
  const c = [1, 3, 5]
    .map((i) => parseInt(hex.slice(i, i + 2), 16) / 255)
    .map((v) => (v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4));
  return 0.2126 * c[0]! + 0.7152 * c[1]! + 0.0722 * c[2]!;
}
function ratio(a: string, b: string): number {
  const [x, y] = [lum(a), lum(b)].sort((p, q) => q - p);
  return (x! + 0.05) / (y! + 0.05);
}

const TEXT: [string, string][] = [
  ["fg", "bg"],
  ["fg", "surface"],
  ["muted", "bg"],
  ["muted", "surface"],
  ["primary-fg", "primary"],
  ["primary-fg", "primary-hover"],
  ["danger", "bg"],
  ["danger", "danger-bg"],
  ["warn-fg", "warn-bg"],
  ["info-fg", "info-bg"],
  ["fg", "danger-bg"],
];
const UI: [string, string][] = [
  ["border", "bg"],
  ["border", "surface"],
  ["focus", "bg"],
  ["focus", "surface"],
  ["primary", "bg"],
  ["danger", "bg"],
];

describe.each([
  ["light", light],
  ["dark", dark],
])("%s palette", (_name, p) => {
  it.each(TEXT)("text %s on %s is at least 4.5:1", (fg, bg) => {
    expect(ratio(p[fg]!, p[bg]!)).toBeGreaterThanOrEqual(4.5);
  });
  it.each(UI)("UI %s against %s is at least 3:1", (a, b) => {
    expect(ratio(p[a]!, p[b]!)).toBeGreaterThanOrEqual(3);
  });
});

describe("palette completeness", () => {
  it("the dark palette defines every token the light one does", () => {
    const darkOnly = palette(css.slice(darkStart, css.indexOf("@theme inline")));
    expect(Object.keys(darkOnly).sort()).toEqual(
      Object.keys(light)
        .filter((k) => k !== "bg" || true)
        .sort(),
    );
  });
});
