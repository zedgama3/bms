import type { RouteObject } from "react-router";
import type { NavEntry, ScreenSet } from "./types";

/** A bad screen set: a duplicate, invalid or reserved name, an absolute or untitled route,
 *  a missing `en` catalogue, or a malformed navigation entry (F19 to F21). */
export class RegistryError extends Error {
  constructor(
    readonly setName: string,
    message: string,
  ) {
    super(`screen set ${JSON.stringify(setName)}: ${message}`);
    this.name = "RegistryError";
  }
}

/** The names the shell keeps for itself (decision 1). */
export const RESERVED_NAMES: readonly string[] = [
  "api",
  "assets",
  "admin",
  "account",
  "sign-in",
  "healthz",
  "readyz",
  "modules",
  "core",
  "demo",
];

/** Reserved names a core area may still use: the shell keeps `demo` from modules, and core/demo
 *  is the demo's own screen set (D53). */
const CORE_MAY_USE = new Set(["demo"]);

const NAME = /^[a-z][a-z0-9]{1,31}$/;

function checkRoutes(set: string, routes: readonly RouteObject[], trail: string): void {
  for (const r of routes) {
    const here = `${trail}/${r.path ?? (r.index ? "(index)" : "(layout)")}`;
    if (typeof r.path === "string") {
      if (r.path.startsWith("/"))
        throw new RegistryError(set, `route ${here} has an absolute path`);
      if (r.path.split("/").includes(".."))
        throw new RegistryError(set, `route ${here} has a ".." segment`);
    }
    // A loader or action runs before the gate's sign-in redirect and so calls the API for a
    // signed-out visitor (F35). Screens fetch inside their components, behind the gate.
    if (r.loader !== undefined || r.action !== undefined) {
      throw new RegistryError(set, `route ${here} has a loader or action; fetch in the component`);
    }
    const kids = r.children ?? [];
    if (kids.length === 0) {
      const title = (r.handle as { title?: unknown } | undefined)?.title;
      if (typeof title !== "string" || title === "") {
        throw new RegistryError(set, `route ${here} has no handle.title`);
      }
      const access = (r.handle as { access?: unknown }).access;
      if (access !== undefined && access !== "public" && access !== "signed-in") {
        throw new RegistryError(set, `route ${here} has an unknown handle.access`);
      }
    } else {
      checkRoutes(set, kids, here);
    }
  }
}

function checkNav(set: string, nav: readonly NavEntry[]): void {
  for (const n of nav) {
    if (n.to.startsWith("/") || n.to.split("/").includes("..")) {
      throw new RegistryError(set, `nav entry ${JSON.stringify(n.to)} must be a relative path`);
    }
    if (n.area !== "main" && n.area !== "admin")
      throw new RegistryError(set, `nav entry ${n.to} has no area`);
    const a = n.access;
    const ok =
      a === undefined ||
      a === "public" ||
      a === "signed-in" ||
      (Array.isArray(a) && a.length > 0 && a.every((p) => typeof p === "string" && p !== ""));
    if (!ok) throw new RegistryError(set, `nav entry ${n.to} has a malformed access`);
  }
}

/** A usage action name (telemetry.md T1, F51). */
export const ACTION_NAME = /^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*){1,2}$/;
const MAX_ACTION_BYTES = 64;

function checkActions(set: string, actions: readonly string[] | undefined): void {
  const seen = new Set<string>();
  for (const a of actions ?? []) {
    if (
      typeof a !== "string" ||
      !ACTION_NAME.test(a) ||
      new TextEncoder().encode(a).length > MAX_ACTION_BYTES
    )
      throw new RegistryError(set, `action ${JSON.stringify(a)} is not a valid action name`);
    if (seen.has(a)) throw new RegistryError(set, `action ${JSON.stringify(a)} is declared twice`);
    seen.add(a);
  }
}

/** Checks every set (F19 to F21) and returns the frozen list the frame is built from. */
export function validateRegistry(sets: readonly ScreenSet[]): readonly ScreenSet[] {
  const seen = new Set<string>();
  for (const s of sets) {
    if (!NAME.test(s.name))
      throw new RegistryError(s.name, "the name must match [a-z][a-z0-9]{1,31}");
    if (RESERVED_NAMES.includes(s.name) && !(s.kind === "core" && CORE_MAY_USE.has(s.name)))
      throw new RegistryError(s.name, "the name is reserved by the shell");
    if (seen.has(s.name)) throw new RegistryError(s.name, "the name is used by another set");
    seen.add(s.name);
    if (s.kind !== "module" && s.kind !== "core")
      throw new RegistryError(s.name, "kind must be module or core");
    if (typeof s.messages["en"] !== "function")
      throw new RegistryError(s.name, "messages.en is required");
    checkRoutes(s.name, s.routes, "");
    checkNav(s.name, s.nav);
    checkActions(s.name, s.actions);
  }
  return Object.freeze([...sets]);
}

/** The sets whose screens are mounted: every core area, and each module GET /api/modules
 *  lists as enabled. */
export function enabledSets(
  sets: readonly ScreenSet[],
  enabledModules: ReadonlySet<string>,
): ScreenSet[] {
  return sets.filter((s) => s.kind === "core" || enabledModules.has(s.name));
}
