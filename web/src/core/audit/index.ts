import type { ScreenSet } from "@/registry/types";

/** The audit log screen (audit.md "UI"). Routes are relative to `/audit/`. */
export const ui: ScreenSet = {
  name: "audit",
  kind: "core",
  routes: [
    {
      index: true,
      lazy: async () => {
        const { AuditLog } = await import("./screens/AuditLog");
        return { Component: AuditLog };
      },
      handle: { title: "core.audit.title" },
    },
  ],
  nav: [{ label: "core.audit.nav", to: "", area: "admin", access: ["audit.search"] }],
  messages: { en: () => import("./messages/en.json").then((m) => m.default) },
};
