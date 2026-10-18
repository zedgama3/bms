import type { ScreenSet } from "@/registry/types";

/** The people screens (people.md "UI"): the person's own profile, people search, a new
 *  person, a person's page and the deletion requests. Routes are relative to `/people/`. */
export const ui: ScreenSet = {
  name: "people",
  kind: "core",
  routes: [
    {
      index: true,
      lazy: async () => {
        const { People } = await import("./screens/People");
        return { Component: People };
      },
      handle: { title: "core.people.search.title" },
    },
    {
      path: "me",
      lazy: async () => {
        const { MyProfile } = await import("./screens/MyProfile");
        return { Component: MyProfile };
      },
      handle: { title: "core.people.me.title" },
    },
    {
      path: "new",
      lazy: async () => {
        const { NewPerson } = await import("./screens/NewPerson");
        return { Component: NewPerson };
      },
      handle: { title: "core.people.new.title" },
    },
    {
      path: "admin/requests",
      lazy: async () => {
        const { Requests } = await import("./screens/Requests");
        return { Component: Requests };
      },
      handle: { title: "core.people.requests.title" },
    },
    {
      path: ":person",
      lazy: async () => {
        const { PersonPage } = await import("./screens/PersonPage");
        return { Component: PersonPage };
      },
      handle: { title: "core.people.person.title" },
    },
  ],
  nav: [
    { label: "core.people.nav.me", to: "me", area: "main" },
    {
      label: "core.people.nav.people",
      to: "",
      area: "main",
      access: ["people.directory.read"],
    },
    {
      label: "core.people.nav.requests",
      to: "admin/requests",
      area: "admin",
      access: ["people.data.delete"],
    },
  ],
  messages: { en: () => import("./messages/en.json").then((m) => m.default) },
};
