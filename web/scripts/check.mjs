// `pnpm check`: every web check in order (web-shell.md "Build and tooling"), stopping at the
// first failure. Run through the package manager that started it, so it works whether or not
// `pnpm` is on PATH (a corepack install, a CI image).
import { spawnSync } from "node:child_process";

const STEPS = [
  "generate:messages:check", // keys.d.ts is current
  "lint", // eslint (boundaries, a11y, hooks, logical properties, no style prop) + prettier
  "typecheck", // tsc --noEmit, strict
  "test", // vitest with coverage
  "imports", // module isolation script
  "openapi:lint", // spectral over openapi.json
  "licenses", // allowlist
  "build", // vite build
  "bundle-size", // budget
];

const pnpm = process.env["npm_execpath"] ?? "pnpm";
for (const step of STEPS) {
  console.log(`\n== pnpm ${step}`);
  const r = spawnSync(pnpm, ["run", step], { stdio: "inherit" });
  if (r.status !== 0) {
    console.error(`\ncheck failed at "${step}"`);
    process.exit(r.status ?? 1);
  }
}
console.log("\nweb check: all steps passed");
