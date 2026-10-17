// Every installed package's license must be OSI-approved and in the allowlist (principle 6;
// web-shell.md "Build and tooling"). Reads `pnpm licenses list --json`, whose top-level keys
// are license names or SPDX expressions. Exits 1 and lists the offenders otherwise.
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

export const ALLOWED = new Set([
  "MIT",
  "ISC",
  "BSD-2-Clause",
  "BSD-3-Clause",
  "Apache-2.0",
  "MPL-2.0",
  "0BSD",
  "Unlicense",
  "CC0-1.0",
  "BlueOak-1.0.0",
  // Not in the spec's list, both OSI-approved: MIT-0 (@csstools packages under jsdom) and
  // Python-2.0 (`argparse` 2.x, a Spectral dependency). Reported to the owner.
  "MIT-0",
  "Python-2.0",
]);

/** Development-time data packages with a non-software license that never reach the build.
 *  caniuse-lite (CC-BY-4.0, browser-support data) is pulled in by eslint-plugin-react-hooks
 *  through @babel/core. An exception applies only when the package is absent from the
 *  production dependency tree. Reported to the owner as an open question. */
export const DEV_ONLY_EXCEPTIONS = new Map([["caniuse-lite", "CC-BY-4.0"]]);

/** True when a license name or SPDX expression is acceptable: for "A OR B" any side, for "A AND B" every side. */
export function licenseAllowed(expr, allowed = ALLOWED) {
  const e = expr
    .trim()
    .replace(/^\((.*)\)$/, "$1")
    .trim();
  if (/\sOR\s/i.test(e)) return e.split(/\sOR\s/i).some((p) => licenseAllowed(p, allowed));
  if (/\sAND\s/i.test(e)) return e.split(/\sAND\s/i).every((p) => licenseAllowed(p, allowed));
  return allowed.has(e);
}

/** Returns "name@version (license)" for every package outside the allowlist. `prodNames` is
 *  the set of package names in the production tree; a dev-only exception cannot apply to those. */
export function offenders(
  listing,
  allowed = ALLOWED,
  prodNames = new Set(),
  exceptions = DEV_ONLY_EXCEPTIONS,
) {
  const out = [];
  for (const [license, pkgs] of Object.entries(listing)) {
    if (licenseAllowed(license, allowed)) continue;
    for (const p of pkgs) {
      if (exceptions.get(p.name) === license && !prodNames.has(p.name)) continue;
      out.push(`${p.name}@${(p.versions ?? []).join(",")} (${license})`);
    }
  }
  return out;
}

export function packageNames(listing) {
  return new Set(Object.values(listing).flatMap((pkgs) => pkgs.map((p) => p.name)));
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const list = (extra) =>
    JSON.parse(
      execFileSync("corepack", ["pnpm", "licenses", "list", "--json", ...extra], {
        encoding: "utf8",
        maxBuffer: 64 * 1024 * 1024,
      }),
    );
  const bad = offenders(list([]), ALLOWED, packageNames(list(["--prod"])));
  if (bad.length > 0) {
    console.error("licenses outside the allowlist:\n  " + bad.join("\n  "));
    process.exit(1);
  }
  console.log("licenses: all packages are in the allowlist");
}
