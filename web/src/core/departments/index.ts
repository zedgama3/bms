import type { ScreenSet } from "@/registry/types";

/** The department screens (departments-events.md "UI"): the list, a new department and a
 *  department's page. Routes are relative to `/departments/`. */
export const ui: ScreenSet = {
  name: "departments",
  kind: "core",
  routes: [
    {
      index: true,
      lazy: async () => {
        const { DepartmentList } = await import("./screens/DepartmentList");
        return { Component: DepartmentList };
      },
      handle: { title: "core.departments.list.title" },
    },
    {
      path: "new",
      lazy: async () => {
        const { DepartmentNew } = await import("./screens/DepartmentNew");
        return { Component: DepartmentNew };
      },
      handle: { title: "core.departments.new.title" },
    },
    {
      path: ":department",
      lazy: async () => {
        const { DepartmentPage } = await import("./screens/DepartmentPage");
        return { Component: DepartmentPage };
      },
      handle: { title: "core.departments.page.title" },
    },
  ],
  nav: [{ label: "core.departments.nav", to: "", area: "main", access: "signed-in" }],
  messages: { en: () => import("./messages/en.json").then((m) => m.default) },
};
