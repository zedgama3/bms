// @vitest-environment node
// Fixture tests for the build tooling: the message-key generator, the bundle budget, the
// license allowlist and the module-isolation script (web-shell.md "Test plan", Tooling).
import { execFileSync, spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { gzipSync } from "node:zlib";
import { describe, expect, it, beforeEach, afterEach } from "vitest";
import { checkBundle, externalOrigins } from "./check-bundle-size.mjs";
import { licenseAllowed, offenders, ALLOWED } from "./check-licenses.mjs";
import { generate } from "./generate-messages.mjs";

let tmp;
beforeEach(() => {
  tmp = fs.mkdtempSync(path.join(os.tmpdir(), "bms-web-tooling-"));
});
afterEach(() => {
  fs.rmSync(tmp, { recursive: true, force: true });
});

function write(rel, content) {
  const p = path.join(tmp, rel);
  fs.mkdirSync(path.dirname(p), { recursive: true });
  fs.writeFileSync(p, content);
}

describe("generate-messages (F22)", () => {
  it("keys.d.ts lists every en key and nothing else", () => {
    write("shell/messages/en.json", JSON.stringify({ "shell.b": "B", "shell.a": "A" }));
    write("modules/x/messages/en.json", JSON.stringify({ "module.x.title": "X" }));
    write(
      "modules/x/messages/es.json",
      JSON.stringify({ "module.x.title": "Equis", "module.x.extra": "no" }),
    );
    const { dts, missing } = generate(tmp);
    expect([...dts.matchAll(/^\s+"([^"]+)": true;$/gm)].map((m) => m[1])).toEqual([
      "module.x.title",
      "shell.a",
      "shell.b",
    ]);
    expect(missing).toEqual({ es: ["shell.a", "shell.b"] }); // a set that lacks a language falls back to en
  });

  it("reports the keys another language lacks, as data, not as a failure", () => {
    write("shell/messages/en.json", JSON.stringify({ "shell.a": "A", "shell.b": "B" }));
    write("shell/messages/es.json", JSON.stringify({ "shell.a": "A" }));
    expect(generate(tmp).missing).toEqual({ es: ["shell.b"] });
  });

  it("fails when a set has no en.json, a key is defined twice, or the file is not flat strings", () => {
    write("shell/messages/en.json", JSON.stringify({ "shell.a": "A" }));
    write("modules/x/messages/es.json", "{}");
    expect(() => generate(tmp)).toThrow(/en\.json is required/);
    fs.rmSync(path.join(tmp, "modules"), { recursive: true });
    write("modules/y/messages/en.json", JSON.stringify({ "shell.a": "dup" }));
    expect(() => generate(tmp)).toThrow(/defined in/);
    fs.rmSync(path.join(tmp, "modules"), { recursive: true });
    write("modules/z/messages/en.json", JSON.stringify({ k: { nested: "no" } }));
    expect(() => generate(tmp)).toThrow(/must be a string/);
  });

  it("the committed keys.d.ts is current", () => {
    const here = path.dirname(new URL(import.meta.url).pathname);
    const { dts } = generate(path.join(here, "..", "src"));
    expect(fs.readFileSync(path.join(here, "..", "src", "i18n", "keys.d.ts"), "utf8")).toBe(dts);
  });
});

describe("check-bundle-size (F36)", () => {
  function bundle({ entryBytes, lazyBytes, cssBytes }) {
    const noise = (n) =>
      Buffer.from(Array.from({ length: n }, () => Math.floor(Math.random() * 256)));
    write("dist/assets/index.js", noise(entryBytes));
    write("dist/assets/lazy.js", noise(lazyBytes));
    write("dist/assets/index.css", noise(cssBytes));
    write(
      "dist/.vite/manifest.json",
      JSON.stringify({
        "index.html": {
          file: "assets/index.js",
          isEntry: true,
          css: ["assets/index.css"],
          dynamicImports: ["lazy.ts"],
        },
        "lazy.ts": { file: "assets/lazy.js", isDynamicEntry: true },
      }),
    );
    return checkBundle(path.join(tmp, "dist"));
  }

  it("passes within budget", () => {
    expect(bundle({ entryBytes: 50_000, lazyBytes: 20_000, cssBytes: 5_000 }).violations).toEqual(
      [],
    );
  });
  it("fails an entry over 200 KB gzip", () => {
    expect(bundle({ entryBytes: 210_000, lazyBytes: 10, cssBytes: 10 }).violations.join()).toMatch(
      /entry JS/,
    );
  });
  it("fails CSS over 30 KB gzip", () => {
    expect(bundle({ entryBytes: 10, lazyBytes: 10, cssBytes: 40_000 }).violations.join()).toMatch(
      /entry CSS/,
    );
  });
  it("fails a lazy chunk over 100 KB gzip", () => {
    expect(bundle({ entryBytes: 10, lazyBytes: 120_000, cssBytes: 10 }).violations.join()).toMatch(
      /lazy chunk assets\/lazy\.js/,
    );
  });
  it("lists external origins in the built files", () => {
    write(
      "dist/assets/a.js",
      'fetch("https://cdn.example.test/x.js"); // http://www.w3.org/2000/svg',
    );
    expect(externalOrigins(path.join(tmp, "dist"))).toEqual([
      "http://www.w3.org",
      "https://cdn.example.test",
    ]);
    expect(gzipSync("x").length).toBeGreaterThan(0);
  });
});

describe("check-licenses (F36)", () => {
  it("accepts the allowlist and SPDX expressions", () => {
    for (const l of ALLOWED) expect(licenseAllowed(l)).toBe(true);
    expect(licenseAllowed("(MIT OR GPL-3.0)")).toBe(true);
    expect(licenseAllowed("MIT AND Zlib")).toBe(false);
    expect(licenseAllowed("(MIT AND ISC)")).toBe(true);
  });
  it("refuses copyleft and unknown licenses", () => {
    expect(licenseAllowed("GPL-3.0")).toBe(false);
    expect(licenseAllowed("AGPL-3.0-only")).toBe(false);
    expect(licenseAllowed("SEE LICENSE IN LICENSE.txt")).toBe(false);
    expect(licenseAllowed("UNLICENSED")).toBe(false);
  });
  it("lists offenders by name, and a dev-only exception never applies to production packages", () => {
    const listing = {
      MIT: [{ name: "a", versions: ["1"] }],
      "GPL-3.0": [{ name: "bad", versions: ["2.0.0"] }],
      "CC-BY-4.0": [{ name: "caniuse-lite", versions: ["1"] }],
    };
    expect(offenders(listing)).toEqual(
      ["bad@2.0.0 (GPL-3.0)"].concat([]).filter(Boolean).length ? ["bad@2.0.0 (GPL-3.0)"] : [],
    );
    expect(offenders(listing, ALLOWED, new Set(["caniuse-lite"]))).toEqual([
      "bad@2.0.0 (GPL-3.0)",
      "caniuse-lite@1 (CC-BY-4.0)",
    ]);
  });
});

describe("check-web-module-imports.sh (F37)", () => {
  const script = path.join(
    path.dirname(new URL(import.meta.url).pathname),
    "../../scripts/check-web-module-imports.sh",
  );
  const run = () => spawnSync("bash", [script, tmp], { encoding: "utf8" });

  it("passes a set that imports only the allowed packages and itself", () => {
    write(
      "modules/ok/index.ts",
      `import type { ScreenSet } from "@/registry/types";\nimport("./screens/A");\n`,
    );
    write(
      "modules/ok/screens/A.tsx",
      `import { api } from "@/api";\nimport { useT } from "@/i18n";\nimport { Button } from "@/ui";\nimport en from "../messages/en.json";\napi.GET("/api/ok/thing");\napi.GET("/api/modules");\n`,
    );
    const r = run();
    expect(r.status, r.stderr).toBe(0);
  });

  it.each([
    ["another module", `import x from "@/modules/other";`, /@\/modules\/other/],
    ["a core area", `import x from "@/core/people";`, /@\/core\/people/],
    ["a frame-private subpath", `import { setSession } from "@/api/internal";`, /@\/api\/internal/],
    ["the frame", `import { useSession } from "@/shell/session";`, /@\/shell\/session/],
    [
      "the registry internals",
      `import { validateRegistry } from "@/registry/registry";`,
      /@\/registry\/registry/,
    ],
    [
      "a relative path out of the set",
      `import x from "../../other/index";`,
      /leaves the set's folder/,
    ],
    ["another module's API", `fetchIt("/api/other/things");`, /\/api\/other/],
  ])("refuses %s", (_what, line, msg) => {
    write("modules/bad/index.ts", `${line}\n`);
    const r = run();
    expect(r.status).toBe(1);
    expect(r.stderr).toMatch(msg);
    expect(r.stderr).toContain("modules/bad/index.ts:1");
  });

  it("applies to core areas the same way", () => {
    write("core/people/index.ts", `import x from "@/modules/hello";`);
    expect(run().status).toBe(1);
  });

  it("ignores test files and runs on the real tree", () => {
    write("modules/ok/a.test.tsx", `import x from "@/shell/session";`);
    expect(run().status).toBe(0);
    const here = path.dirname(new URL(import.meta.url).pathname);
    expect(
      execFileSync("bash", [script, path.join(here, "..", "src")], { encoding: "utf8" }),
    ).toContain("ok");
  });
});
