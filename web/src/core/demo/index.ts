import type { ScreenSet } from "@/registry/types";

/** The demo area (demo.md "UI"): the captured-mail screen. Routes are relative to `/demo/`.
 *  No navigation entry: the demo banner links to it. */
export const ui: ScreenSet = {
  name: "demo",
  kind: "core",
  routes: [
    {
      path: "mail",
      lazy: async () => {
        const { Mail } = await import("./screens/Mail");
        return { Component: Mail };
      },
      handle: { title: "core.demo.mail.title" },
    },
  ],
  nav: [],
  messages: { en: () => import("./messages/en.json").then((m) => m.default) },
};
