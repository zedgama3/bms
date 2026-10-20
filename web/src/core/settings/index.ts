import type { ScreenSet } from "@/registry/types";

/** The settings screens (settings.md "UI"): the regional's own values, terminology, and one
 *  form per other area. Routes are relative to `/settings/`. */
export const ui: ScreenSet = {
  name: "settings",
  kind: "core",
  routes: [
    {
      index: true,
      lazy: async () => {
        const { Regional } = await import("./screens/Regional");
        return { Component: Regional };
      },
      handle: { title: "core.settings.title" },
    },
    {
      path: "terms",
      lazy: async () => {
        const { Terms } = await import("./screens/Terms");
        return { Component: Terms };
      },
      handle: { title: "core.settings.terms.title" },
    },
    {
      path: ":area",
      lazy: async () => {
        const { Area } = await import("./screens/Area");
        return { Component: Area };
      },
      handle: { title: "core.settings.area_page.title" },
    },
  ],
  nav: [{ label: "core.settings.nav", to: "", area: "admin", access: ["settings.read"] }],
  messages: { en: () => import("./messages/en.json").then((m) => m.default) },
};
