import type { ScreenSet } from "@/registry/types";

/** The reference module's screens (module-hello.md "UI"). */
export const ui: ScreenSet = {
  name: "hello",
  kind: "module",
  routes: [
    {
      index: true,
      lazy: async () => {
        const { Greeting } = await import("./screens/Greeting");
        return { Component: Greeting };
      },
      handle: { title: "module.hello.title", access: "public" },
    },
  ],
  nav: [{ label: "module.hello.nav.greeting", to: "", area: "main", access: "public" }],
  messages: { en: () => import("./messages/en.json").then((m) => m.default) },
};
