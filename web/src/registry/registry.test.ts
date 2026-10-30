import { describe, expect, it } from "vitest";
import type { RouteObject } from "react-router";
import { coreSets } from "@/core";
import { moduleSets } from "@/modules";
import { enabledSets, RegistryError, RESERVED_NAMES, validateRegistry } from "./registry";
import type { ScreenSet } from "./types";
import { canSee } from "@/shell/session";
import { navItems } from "@/shell/registryNav";
import { buildRoutes } from "@/shell/routes";

const leaf: RouteObject = { index: true, handle: { title: "shell.home.title" } };

function set(over: Partial<ScreenSet> = {}): ScreenSet {
  return {
    name: "abc",
    kind: "module",
    routes: [leaf],
    nav: [],
    messages: { en: () => Promise.resolve({}) },
    ...over,
  };
}

describe("validateRegistry", () => {
  it("F19: duplicate, invalid and reserved names are refused", () => {
    expect(() => validateRegistry([set(), set()])).toThrow(RegistryError);
    expect(() => validateRegistry([set({ name: "Abc" })])).toThrow(/name must match/);
    expect(() => validateRegistry([set({ name: "a" })])).toThrow(/name must match/);
    expect(() => validateRegistry([set({ name: "a".repeat(33) })])).toThrow(/name must match/);
    expect(() => validateRegistry([set({ name: "1abc" })])).toThrow(/name must match/);
    for (const reserved of RESERVED_NAMES) {
      // names with a dash fail the grammar first; either way they are refused
      expect(() => validateRegistry([set({ name: reserved })]), reserved).toThrow(RegistryError);
    }
    expect(() => validateRegistry([set({ name: "admin" })])).toThrow(/reserved/);
  });

  it("names the offending set", () => {
    try {
      validateRegistry([set({ name: "core" })]);
      expect.unreachable();
    } catch (e) {
      expect((e as RegistryError).setName).toBe("core");
    }
  });

  it("F20: absolute or untitled routes are refused", () => {
    expect(() =>
      validateRegistry([set({ routes: [{ path: "/x", handle: { title: "t" } }] })]),
    ).toThrow(/absolute/);
    expect(() =>
      validateRegistry([set({ routes: [{ path: "a/../b", handle: { title: "t" } }] })]),
    ).toThrow(/"\.\."/);
    expect(() => validateRegistry([set({ routes: [{ path: "x" }] })])).toThrow(/handle\.title/);
    expect(() =>
      validateRegistry([set({ routes: [{ path: "x", handle: { title: "" } }] })]),
    ).toThrow(/handle\.title/);
    expect(() =>
      validateRegistry([
        set({ routes: [{ path: "x", handle: { title: "t", access: "everyone" } }] }),
      ]),
    ).toThrow(/access/);
  });

  it("a parent route needs no title but its leaves do", () => {
    expect(
      validateRegistry([
        set({ routes: [{ path: "p", children: [{ path: "c", handle: { title: "t" } }] }] }),
      ]),
    ).toHaveLength(1);
    expect(() =>
      validateRegistry([set({ routes: [{ path: "p", children: [{ path: "c" }] }] })]),
    ).toThrow(/handle\.title/);
  });

  it("refuses a loader or an action (F35: nothing runs before the sign-in redirect)", () => {
    expect(() => validateRegistry([set({ routes: [{ ...leaf, loader: () => null }] })])).toThrow(
      /loader or action/,
    );
    expect(() =>
      validateRegistry([
        set({ routes: [{ path: "p", children: [{ ...leaf, action: () => null }] }] }),
      ]),
    ).toThrow(/loader or action/);
  });

  it("F51: bad action names are refused", () => {
    const ok = ["shift.filter", "dialog.open_help", "a1.b2.c3"];
    expect(validateRegistry([set({ actions: ok })])).toHaveLength(1);
    expect(validateRegistry([set()])).toHaveLength(1);
    for (const bad of [
      "single",
      "Upper.case",
      "a.b.c.d",
      "1a.b",
      "a..b",
      "a.b-c",
      "",
      "a.b ",
      "a." + "x".repeat(70),
    ]) {
      expect(() => validateRegistry([set({ actions: [bad] })]), bad).toThrow(RegistryError);
    }
    expect(() => validateRegistry([set({ actions: ["a.b", "a.b"] })])).toThrow(/declared twice/);
  });

  it("D53: `demo` is reserved for modules but core/demo may use it", () => {
    expect(() => validateRegistry([set({ name: "demo" })])).toThrow(/reserved/);
    expect(validateRegistry([set({ name: "demo", kind: "core" })])).toHaveLength(1);
    expect(() => validateRegistry([set({ name: "admin", kind: "core" })])).toThrow(/reserved/);
  });

  it("F21: a set without en is refused", () => {
    expect(() => validateRegistry([set({ messages: { es: () => Promise.resolve({}) } })])).toThrow(
      /messages\.en/,
    );
  });

  it("refuses an absolute nav path and a malformed access", () => {
    expect(() =>
      validateRegistry([set({ nav: [{ label: "shell.home.title", to: "/x", area: "main" }] })]),
    ).toThrow(/relative/);
    expect(() =>
      validateRegistry([
        set({ nav: [{ label: "shell.home.title", to: "x", area: "main", access: [] }] }),
      ]),
    ).toThrow(/access/);
  });

  it("freezes the list it returns", () => {
    expect(Object.isFrozen(validateRegistry([set()]))).toBe(true);
  });

  it("the real lists validate", () => {
    const all = validateRegistry([...coreSets, ...moduleSets]);
    expect(all.map((s) => s.name)).toContain("hello");
  });

  it("hello is the reference: titled public route, nav entry, en catalogue", async () => {
    const hello = moduleSets.find((s) => s.name === "hello")!;
    expect(hello.kind).toBe("module");
    expect(hello.nav).toEqual([
      { label: "module.hello.nav.greeting", to: "", area: "main", access: "public" },
    ]);
    const en = await hello.messages["en"]!();
    expect(en).toHaveProperty("module.hello.title");
    expect(en).toHaveProperty("module.hello.nav.greeting");
  });
});

describe("enabledSets and the router", () => {
  const modules = set({ name: "mod" });
  const core = set({ name: "area", kind: "core" });

  it("core sets ignore enablement; modules need to be listed as enabled", () => {
    expect(enabledSets([modules, core], new Set()).map((s) => s.name)).toEqual(["area"]);
    expect(enabledSets([modules, core], new Set(["mod"])).map((s) => s.name)).toEqual([
      "mod",
      "area",
    ]);
  });

  it("a disabled module's routes are not mounted and its nav is absent", () => {
    const paths = (sets: ScreenSet[]) =>
      (buildRoutes(sets)[0]!.children![0]!.children as RouteObject[]).map(
        (r) => r.path ?? "(index)",
      );
    expect(paths(enabledSets(moduleSets, new Set()))).not.toContain("hello");
    expect(paths(enabledSets(moduleSets, new Set(["hello"])))).toContain("hello");
    expect(paths(enabledSets(moduleSets, new Set()))).toContain("*");
    const signedOut = { signedIn: false, signupOpen: false } as const;
    expect(navItems(enabledSets(moduleSets, new Set()), signedOut).map((i) => i.to)).toEqual([]);
    expect(
      navItems(enabledSets(moduleSets, new Set(["hello"])), signedOut).map((i) => i.to),
    ).toEqual(["/hello"]);
  });
});

describe("access filtering of navigation (a convenience, never access control)", () => {
  const out = { signedIn: false, signupOpen: false } as const;
  const inNoPerms = {
    signedIn: true,
    personId: "p",
    accountId: "a",
    playaName: "n",
    superuser: false,
    passwordChangeRequired: false,
    expiresAt: null,
    signupOpen: false,
    permissions: [],
  } as const;
  const inPerms = { ...inNoPerms, permissions: ["core.modules.read"] } as const;
  const inOther = { ...inNoPerms, permissions: ["something.else"] } as const;

  it("public is always visible; signed-in needs a session", () => {
    expect(canSee(out, "public")).toBe(true);
    expect(canSee(out, "signed-in")).toBe(false);
    expect(canSee(out, undefined)).toBe(false);
    expect(canSee(inNoPerms, undefined)).toBe(true);
  });

  it("a permission list shows to a holder; an empty permission list means none", () => {
    expect(canSee(out, ["core.modules.read"])).toBe(false);
    expect(canSee(inPerms, ["core.modules.read"])).toBe(true);
    expect(canSee(inOther, ["core.modules.read"])).toBe(false);
    expect(canSee(inNoPerms, ["core.modules.read"])).toBe(false);
  });

  it("matches `p` exactly or any `p@<department>`, never a longer name or another prefix", () => {
    const dept = { ...inNoPerms, permissions: ["access.grants.read@dept-1"] } as const;
    expect(canSee(dept, ["access.grants.read"])).toBe(true);
    expect(canSee(dept, ["access.grants"])).toBe(false);
    expect(canSee(dept, ["access.grants.read.more"])).toBe(false);
    expect(
      canSee({ ...inNoPerms, permissions: ["access.grants.readx"] }, ["access.grants.read"]),
    ).toBe(false);
    expect(canSee(inPerms, [])).toBe(false);
  });
});
