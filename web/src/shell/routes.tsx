import type { RouteObject } from "react-router";
import type { ScreenSet } from "@/registry/types";
import { CrashedScreen } from "./ErrorBoundary";
import { Frame } from "./Frame";
import { Loading } from "@/ui";
import { AdminModules } from "./screens/AdminModules";
import { Home } from "./screens/Home";
import { NotFound } from "./screens/NotFound";
import { SignInRedirect } from "./screens/SignInRedirect";

/** The router's routes: the shell's own screens, then each enabled set under `/<name>`, then not found. */
export function buildRoutes(sets: readonly ScreenSet[]): RouteObject[] {
  return [
    {
      id: "root",
      element: <Frame />,
      HydrateFallback: Loading,
      children: [
        {
          errorElement: <CrashedScreen />,
          children: [
            {
              index: true,
              element: <Home />,
              handle: { title: "shell.home.title", access: "public" },
            },
            {
              path: "sign-in",
              element: <SignInRedirect />,
              handle: { title: "shell.signin", access: "public" },
            },
            {
              path: "admin/modules",
              element: <AdminModules />,
              handle: { title: "shell.admin.modules.title" },
            },
            ...sets.map((s) => ({ path: s.name, children: [...s.routes] })),
            {
              path: "*",
              element: <NotFound />,
              handle: { title: "shell.notfound.title", access: "public" },
            },
          ],
        },
      ],
    },
  ];
}
