import { matchRoutes } from "react-router";
import { describe, expect, it } from "vitest";
import { coreSets } from "@/core";
import { moduleSets } from "@/modules";
import { validateRegistry } from "@/registry/registry";
import type { ScreenSet } from "@/registry/types";
import { patternOf } from "./boot";
import { clientManifest, SHELL_SET } from "./manifest";
import { buildRoutes } from "./routes";

const sets = validateRegistry([...coreSets, ...moduleSets]);

/** A URL that matches a pattern: `{id}` becomes "x1", `{rest}` a two-segment path. */
function concrete(pattern: string): string {
  return pattern.replace("{rest}", "no/where").replace(/\{[a-z_]+\}/g, "x1");
}

describe("client-manifest.json (F52)", () => {
  it("F52: equals the router's patterns", () => {
    const manifest = clientManifest(sets);
    const routes = buildRoutes(sets);
    expect(manifest.screens.length).toBeGreaterThan(10);

    // Every manifest screen is a real route, and the pattern the usage collector computes for
    // a visit to it (patternOf) is that same pattern.
    for (const { pattern } of manifest.screens) {
      const url = concrete(pattern);
      const hit = matchRoutes(routes, url);
      expect(hit, url).not.toBeNull();
      const last = hit![hit!.length - 1]!;
      expect(patternOf(url, last.params), url).toBe(pattern);
    }

    // And no router leaf is missing: every route the router has is in the manifest.
    const leaves: string[] = [];
    const walk = (rs: ReturnType<typeof buildRoutes>, base: string) => {
      for (const r of rs) {
        const path = r.path ? `${base === "/" ? "" : base}/${r.path}` : base;
        if (r.children) walk(r.children, path);
        else leaves.push(path.replace(/:([A-Za-z0-9_]+)/g, "{$1}").replace(/\*/g, "{rest}") || "/");
      }
    };
    walk(routes, "/");
    expect(manifest.screens.map((s) => s.pattern).sort()).toEqual([...new Set(leaves)].sort());
  });

  it("names the owning set: the shell's own screens are `shell`", () => {
    const by = new Map(clientManifest(sets).screens.map((s) => [s.pattern, s.set]));
    expect(by.get("/")).toBe(SHELL_SET);
    expect(by.get("/admin/modules")).toBe(SHELL_SET);
    expect(by.get("/accounts/sign-in")).toBe("accounts");
    expect(by.get("/demo/mail")).toBe("demo");
    expect(by.get("/{rest}")).toBe(SHELL_SET);
  });

  it("lists every set's declared actions with their owner", () => {
    const withActions: ScreenSet = {
      name: "abc",
      kind: "module",
      routes: [{ index: true, handle: { title: "shell.home.title" } }],
      nav: [],
      messages: { en: () => Promise.resolve({}) },
      actions: ["shift.filter", "dialog.open"],
    };
    const m = clientManifest(validateRegistry([withActions]));
    expect(m.actions).toEqual([
      { name: "shift.filter", set: "abc" },
      { name: "dialog.open", set: "abc" },
    ]);
    expect(m.screens).toContainEqual({ pattern: "/abc", set: "abc" });
  });
});
