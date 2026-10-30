import { render } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { createMemoryRouter } from "react-router";
import type { DataRouter } from "react-router";
import { vi } from "vitest";
import { createQueryClient } from "@/api/internal";
import { addCatalogue, configureLanguages } from "@/i18n/internal";
import { coreSets } from "@/core";
import { moduleSets } from "@/modules";
import type { ScreenSet } from "@/registry/types";
import { App } from "@/shell/App";
import shellEn from "@/shell/messages/en.json";
import { resetPromptForTest } from "@/shell/me";
import { resetPublicForTest } from "@/shell/publicDoc";
import { resetRegionalForTest } from "@/shell/regional";
import { setSession, SIGNED_OUT } from "@/shell/session";
import { setReadOnly } from "@/shell/readonly";
import { wireClient } from "@/shell/wire";
import { installFetch, json, problem } from "./fakeFetch";
import type { Handler } from "./fakeFetch";

export interface Mount {
  /** Responses by "METHOD /path"; anything unlisted is a 404 problem. */
  routes?: Record<string, Handler>;
  modules?: { name: string; enabled: boolean }[];
  /** undefined: the session endpoint answers 404 (M0). */
  session?: Record<string, unknown> | "fail";
  path?: string;
  sets?: readonly ScreenSet[];
  /** When it returns true every response carries X-Read-Only: true. */
  readOnly?: () => boolean;
}

export function mountApp(opts: Mount = {}) {
  resetPromptForTest();
  setSession(SIGNED_OUT);
  setReadOnly(false);
  resetRegionalForTest();
  resetPublicForTest();
  configureLanguages(["en"], "en");
  addCatalogue("shell", "en", shellEn);
  const client = createQueryClient({ retry: false });
  let router: DataRouter | null = null;
  wireClient({ reporter: { apiFailure: vi.fn() }, queryClient: client, getRouter: () => router });
  const fetchMock = installFetch(async (req) => {
    const res = await respond(req);
    if (!opts.readOnly?.()) return res;
    const headers = new Headers(res.headers);
    headers.set("X-Read-Only", "true");
    return new Response(await res.text(), { status: res.status, headers });
  });
  const respond = (req: Request): Response | Promise<Response> => {
    const url = new URL(req.url);
    const hit = opts.routes?.[`${req.method} ${url.pathname}`];
    if (hit) return hit(req);
    if (req.method === "GET" && url.pathname === "/api/modules") {
      return json({ modules: opts.modules ?? [{ name: "hello", enabled: true }] });
    }
    if (req.method === "GET" && url.pathname === "/api/accounts/session") {
      if (opts.session === "fail") return problem(500, { code: "internal", ref: "AAAA-0000" });
      if (opts.session) return json({ signed_in: true, ...opts.session });
      return problem(404, { code: "not_found" });
    }
    if (url.pathname === "/api/hello/public-greeting") {
      return json({
        greeting: "Hello",
        banners: [{ module: "hello", key: "module.hello.banner.welcome" }],
      });
    }
    return problem(404, { code: "not_found" });
  };
  const leave = vi.fn();
  const view = render(
    <QueryClientProvider client={client}>
      <App
        sets={opts.sets ?? [...coreSets, ...moduleSets]}
        leave={leave}
        onRouter={(r) => {
          router = r;
        }}
        createRouter={(routes) =>
          createMemoryRouter(routes, { initialEntries: [opts.path ?? "/"] })
        }
      />
    </QueryClientProvider>,
  );
  return { ...view, client, fetchMock, leave, router: () => router };
}
