// dist/client-manifest.json (telemetry.md T1.5, web-shell.md amendment 2026-10-28): the screen
// patterns and declared action names the server allows usage events for. Derived from the
// routes `buildRoutes(sets)` really mounts, so it cannot drift from the router.
import type { RouteObject } from "react-router";
import type { ScreenSet } from "@/registry/types";
import { buildRoutes } from "./routes";
import { toPattern } from "./report";

/** The owner of the shell's own screens in the manifest. */
export const SHELL_SET = "shell";

export interface ClientManifest {
  screens: { pattern: string; set: string }[];
  actions: { name: string; set: string }[];
}

function join(base: string, path: string | undefined): string {
  if (path === undefined || path === "") return base;
  return `${base === "/" ? "" : base}/${path.replace(/^\/+/, "")}`.replace(/\/+$/, "") || "/";
}

/** Every leaf route of `buildRoutes(sets)` as the pattern the usage collector reports
 *  (`:id` becomes `{id}`, a splat `{rest}`), with the set that owns it. An index route
 *  is its parent's path. */
export function clientManifest(sets: readonly ScreenSet[]): ClientManifest {
  const screens = new Map<string, string>();
  const walk = (routes: readonly RouteObject[], base: string, owner: string) => {
    for (const r of routes) {
      const mounted = sets.find(
        (s) =>
          s.name === r.path &&
          r.children !== undefined &&
          r.children.length === s.routes.length &&
          r.children.every((c, i) => c === s.routes[i]),
      );
      const path = join(base, r.path);
      const who = mounted?.name ?? owner;
      if (r.children !== undefined) {
        walk(r.children, path, who);
        continue;
      }
      const pattern = toPattern(path);
      if (pattern !== undefined && !screens.has(pattern)) screens.set(pattern, who);
    }
  };
  walk(buildRoutes(sets), "/", SHELL_SET);
  const actions: ClientManifest["actions"] = [];
  for (const s of sets) for (const name of s.actions ?? []) actions.push({ name, set: s.name });
  return {
    screens: [...screens].map(([pattern, set]) => ({ pattern, set })),
    actions,
  };
}
