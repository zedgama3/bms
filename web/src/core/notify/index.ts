import type { ScreenSet } from "@/registry/types";

/** The notify admin screens (notify.md "UI"): e-mail templates and the outbox. Routes are
 *  relative to `/notify/`. */
export const ui: ScreenSet = {
  name: "notify",
  kind: "core",
  routes: [
    {
      index: true,
      lazy: async () => {
        const { Index } = await import("./screens/Index");
        return { Component: Index };
      },
      handle: { title: "core.notify.templates.title" },
    },
    {
      path: "templates",
      lazy: async () => {
        const { Templates } = await import("./screens/Templates");
        return { Component: Templates };
      },
      handle: { title: "core.notify.templates.title" },
    },
    {
      path: "templates/:name/:lang",
      lazy: async () => {
        const { TemplateEdit } = await import("./screens/TemplateEdit");
        return { Component: TemplateEdit };
      },
      handle: { title: "core.notify.edit.title" },
    },
    {
      path: "outbox",
      lazy: async () => {
        const { Outbox } = await import("./screens/Outbox");
        return { Component: Outbox };
      },
      handle: { title: "core.notify.outbox.title" },
    },
  ],
  nav: [
    {
      label: "core.notify.nav.templates",
      to: "templates",
      area: "admin",
      access: ["notify.templates.manage"],
    },
    {
      label: "core.notify.nav.outbox",
      to: "outbox",
      area: "admin",
      access: ["notify.outbox.manage"],
    },
  ],
  messages: { en: () => import("./messages/en.json").then((m) => m.default) },
};
