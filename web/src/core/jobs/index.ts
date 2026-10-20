import type { ScreenSet } from "@/registry/types";

/** The background-work status screen (jobs.md "UI"). Routes are relative to `/jobs/`. */
export const ui: ScreenSet = {
  name: "jobs",
  kind: "core",
  routes: [
    {
      index: true,
      lazy: async () => {
        const { Jobs } = await import("./screens/Jobs");
        return { Component: Jobs };
      },
      handle: { title: "core.jobs.title" },
    },
  ],
  nav: [{ label: "core.jobs.nav", to: "", area: "admin", access: ["jobs.read"] }],
  messages: { en: () => import("./messages/en.json").then((m) => m.default) },
};
