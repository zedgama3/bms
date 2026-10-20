import type { ScreenSet } from "@/registry/types";

/** The accounts screens (identity.md "UI"): sign in, sign up, forgotten password, change password, and the
 *  account admin's list and detail. Routes are relative to `/accounts/`. */
export const ui: ScreenSet = {
  name: "accounts",
  kind: "core",
  routes: [
    {
      index: true,
      lazy: async () => {
        const { Index } = await import("./screens/Index");
        return { Component: Index };
      },
      handle: { title: "core.accounts.password.title" },
    },
    {
      path: "sign-in",
      lazy: async () => {
        const { SignIn } = await import("./screens/SignIn");
        return { Component: SignIn };
      },
      handle: { title: "core.accounts.signin.title", access: "public" },
    },
    {
      path: "sign-up",
      lazy: async () => {
        const { SignUp } = await import("./screens/SignUp");
        return { Component: SignUp };
      },
      handle: { title: "core.accounts.signup.title", access: "public" },
    },
    {
      path: "password-reset",
      lazy: async () => {
        const { PasswordReset } = await import("./screens/PasswordReset");
        return { Component: PasswordReset };
      },
      handle: { title: "core.accounts.reset.title", access: "public" },
    },
    {
      path: "password",
      lazy: async () => {
        const { Password } = await import("./screens/Password");
        return { Component: Password };
      },
      handle: { title: "core.accounts.password.title" },
    },
    {
      path: "admin",
      lazy: async () => {
        const { AdminList } = await import("./screens/AdminList");
        return { Component: AdminList };
      },
      handle: { title: "core.accounts.admin.title" },
    },
    {
      path: "admin/:account",
      lazy: async () => {
        const { AdminDetail } = await import("./screens/AdminDetail");
        return { Component: AdminDetail };
      },
      handle: { title: "core.accounts.detail.title" },
    },
  ],
  nav: [
    { label: "core.accounts.nav.account", to: "password", area: "main" },
    { label: "core.accounts.nav.admin", to: "admin", area: "admin", access: ["accounts.read"] },
  ],
  messages: { en: () => import("./messages/en.json").then((m) => m.default) },
};
