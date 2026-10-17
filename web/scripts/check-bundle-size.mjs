// The bundle budget (web-shell.md "Settings"): the shell entry <= 200 KB gzip of JS and
// <= 30 KB gzip of CSS; each lazy chunk <= 100 KB gzip. Reads dist/.vite/manifest.json.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { gzipSync } from "node:zlib";

export const LIMITS = { entryJs: 200 * 1024, entryCss: 30 * 1024, lazyChunk: 100 * 1024 };

function gz(dist, file) {
  return gzipSync(fs.readFileSync(path.join(dist, file))).length;
}

/** Returns a list of violations ("what: size > limit") and a list of measurements. */
export function checkBundle(dist, limits = LIMITS) {
  const manifest = JSON.parse(fs.readFileSync(path.join(dist, ".vite", "manifest.json"), "utf8"));
  const violations = [];
  const report = [];

  // The entry's static closure: the entry chunk and everything it imports statically.
  const staticClosure = new Set();
  const css = new Set();
  const walk = (key) => {
    if (staticClosure.has(key)) return;
    staticClosure.add(key);
    const c = manifest[key];
    if (!c) return;
    for (const f of c.css ?? []) css.add(f);
    for (const k of c.imports ?? []) walk(k);
  };
  const entries = Object.entries(manifest).filter(([, c]) => c.isEntry);
  for (const [key] of entries) walk(key);

  let entryJs = 0;
  for (const key of staticClosure) {
    const c = manifest[key];
    if (c?.file.endsWith(".js")) entryJs += gz(dist, c.file);
  }
  let entryCss = 0;
  for (const f of css) entryCss += gz(dist, f);
  report.push(`entry JS ${(entryJs / 1024).toFixed(1)} KB gzip (limit ${limits.entryJs / 1024})`);
  report.push(
    `entry CSS ${(entryCss / 1024).toFixed(1)} KB gzip (limit ${limits.entryCss / 1024})`,
  );
  if (entryJs > limits.entryJs)
    violations.push(`entry JS: ${entryJs} > ${limits.entryJs} bytes gzip`);
  if (entryCss > limits.entryCss)
    violations.push(`entry CSS: ${entryCss} > ${limits.entryCss} bytes gzip`);

  for (const [key, c] of Object.entries(manifest)) {
    if (staticClosure.has(key) || !c.file.endsWith(".js")) continue;
    const size = gz(dist, c.file);
    report.push(
      `lazy ${c.file} ${(size / 1024).toFixed(1)} KB gzip (limit ${limits.lazyChunk / 1024})`,
    );
    if (size > limits.lazyChunk)
      violations.push(`lazy chunk ${c.file}: ${size} > ${limits.lazyChunk} bytes gzip`);
  }
  return { violations, report };
}

/** Every https:// origin in dist/ must be a documentation link, never a request (decision 15). */
export function externalOrigins(dist) {
  const found = new Set();
  const walk = (dir) => {
    for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
      const p = path.join(dir, e.name);
      if (e.isDirectory()) walk(p);
      else if (/\.(js|css|html)$/.test(e.name)) {
        for (const m of fs.readFileSync(p, "utf8").matchAll(/https?:\/\/[A-Za-z0-9.-]+/g))
          found.add(m[0]);
      }
    }
  };
  walk(dist);
  return [...found].sort();
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const dist = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..", "dist");
  const { violations, report } = checkBundle(dist);
  for (const line of report) console.log(line);
  if (violations.length > 0) {
    console.error("bundle budget exceeded:\n  " + violations.join("\n  "));
    process.exit(1);
  }
}
