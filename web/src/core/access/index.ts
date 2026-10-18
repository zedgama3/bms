import type { ScreenSet } from "@/registry/types";

/** The access screens (authz.md "UI"): roles and their editor, grants, and the separation-of-
 *  duties rules. Routes are relative to `/access/`. */
export const ui: ScreenSet = {
  name: "access",
  kind: "core",
  routes: [
    {
      index: true,
      lazy: async () => {
        const { Index } = await import("./screens/Index");
        return { Component: Index };
      },
      handle: { title: "core.access.roles.title" },
    },
    {
      path: "roles",
      lazy: async () => {
        const { RoleList } = await import("./screens/RoleList");
        return { Component: RoleList };
      },
      handle: { title: "core.access.roles.title" },
    },
    {
      path: "roles/new",
      lazy: async () => {
        const { RoleEditor } = await import("./screens/RoleEditor");
        return { Component: RoleEditor };
      },
      handle: { title: "core.access.editor.new_title" },
    },
    {
      path: "roles/:role",
      lazy: async () => {
        const { RoleEditor } = await import("./screens/RoleEditor");
        return { Component: RoleEditor };
      },
      handle: { title: "core.access.editor.title" },
    },
    {
      path: "grants",
      lazy: async () => {
        const { GrantList } = await import("./screens/GrantList");
        return { Component: GrantList };
      },
      handle: { title: "core.access.grants.title" },
    },
    {
      path: "grants/new",
      lazy: async () => {
        const { GrantNew } = await import("./screens/GrantNew");
        return { Component: GrantNew };
      },
      handle: { title: "core.access.new_grant.title" },
    },
    {
      path: "rules",
      lazy: async () => {
        const { RuleList } = await import("./screens/RuleList");
        return { Component: RuleList };
      },
      handle: { title: "core.access.rules.title" },
    },
  ],
  nav: [
    {
      label: "core.access.nav.roles",
      to: "roles",
      area: "admin",
      access: ["access.roles.read"],
    },
    {
      label: "core.access.nav.grants",
      to: "grants",
      area: "admin",
      access: ["access.grants.read"],
    },
  ],
  messages: { en: () => import("./messages/en.json").then((m) => m.default) },
};
