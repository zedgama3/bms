import type { ScreenSet } from "@/registry/types";

/** The event screens (departments-events.md "UI"): the list, a new event, an event's page and
 *  the event kinds. Routes are relative to `/events/`. */
export const ui: ScreenSet = {
  name: "events",
  kind: "core",
  routes: [
    {
      index: true,
      lazy: async () => {
        const { EventList } = await import("./screens/EventList");
        return { Component: EventList };
      },
      handle: { title: "core.events.list.title" },
    },
    {
      path: "new",
      lazy: async () => {
        const { EventNew } = await import("./screens/EventNew");
        return { Component: EventNew };
      },
      handle: { title: "core.events.new.title" },
    },
    {
      path: "kinds",
      lazy: async () => {
        const { EventKinds } = await import("./screens/EventKinds");
        return { Component: EventKinds };
      },
      handle: { title: "core.events.kinds.title" },
    },
    {
      path: ":event",
      lazy: async () => {
        const { EventPage } = await import("./screens/EventPage");
        return { Component: EventPage };
      },
      handle: { title: "core.events.page.title" },
    },
  ],
  nav: [
    { label: "core.events.nav", to: "", area: "main", access: "signed-in" },
    { label: "core.events.nav_kinds", to: "kinds", area: "admin", access: ["events.manage"] },
  ],
  messages: { en: () => import("./messages/en.json").then((m) => m.default) },
};
