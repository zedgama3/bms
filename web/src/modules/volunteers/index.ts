import type { ScreenSet } from "@/registry/types";
import { LEAD_PERMISSIONS } from "./permissions";

/** The volunteers module's screens (module-volunteers.md "UI"). Routes are relative to
 *  `/volunteers/`. */
export const ui: ScreenSet = {
  name: "volunteers",
  kind: "module",
  routes: [
    {
      index: true,
      lazy: async () => {
        const { Shifts } = await import("./screens/Shifts");
        return { Component: Shifts };
      },
      handle: { title: "module.volunteers.shifts.title" },
    },
    {
      path: "shifts/:shift",
      lazy: async () => {
        const { ShiftPage } = await import("./screens/ShiftPage");
        return { Component: ShiftPage };
      },
      handle: { title: "module.volunteers.shift.title" },
    },
    {
      path: "shifts/:shift/edit",
      lazy: async () => {
        const { ShiftEdit } = await import("./screens/ShiftEdit");
        return { Component: ShiftEdit };
      },
      handle: { title: "module.volunteers.form.edit_title" },
    },
    {
      path: "me",
      lazy: async () => {
        const { MyVolunteering } = await import("./screens/MyVolunteering");
        return { Component: MyVolunteering };
      },
      handle: { title: "module.volunteers.me.title" },
    },
    {
      path: "admin",
      lazy: async () => {
        const { Admin } = await import("./screens/Admin");
        return { Component: Admin };
      },
      handle: { title: "module.volunteers.admin.title" },
    },
    {
      path: "departments/:department",
      lazy: async () => {
        const { DepartmentPage } = await import("./screens/DepartmentPage");
        return { Component: DepartmentPage };
      },
      handle: { title: "module.volunteers.department.title" },
    },
    {
      path: "departments/:department/shifts/new",
      lazy: async () => {
        const { ShiftNew } = await import("./screens/ShiftNew");
        return { Component: ShiftNew };
      },
      handle: { title: "module.volunteers.form.new_title" },
    },
    {
      path: "people/:person",
      lazy: async () => {
        const { PersonQualifications } = await import("./screens/PersonQualifications");
        return { Component: PersonQualifications };
      },
      handle: { title: "module.volunteers.person.title" },
    },
    {
      path: "qualifications",
      lazy: async () => {
        const { OrgQualifications } = await import("./screens/OrgQualifications");
        return { Component: OrgQualifications };
      },
      handle: { title: "module.volunteers.catalogue.title" },
    },
  ],
  nav: [
    { label: "module.volunteers.nav.shifts", to: "", area: "main", access: "signed-in" },
    { label: "module.volunteers.nav.me", to: "me", area: "main", access: "signed-in" },
    { label: "module.volunteers.nav.admin", to: "admin", area: "admin", access: LEAD_PERMISSIONS },
  ],
  messages: { en: () => import("./messages/en.json").then((m) => m.default) },
};
