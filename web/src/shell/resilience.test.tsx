import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it } from "vitest";
import type { ScreenSet } from "@/registry/types";
import { mountApp } from "@/test/app";
import { setupI18n } from "@/test/render";
import { json, problem } from "@/test/fakeFetch";
import { installReporter, uninstallReporterForTest } from "./report";
import type { Batch } from "./report";

afterEach(() => {
  uninstallReporterForTest();
});

const ident = {
  person_id: "7d2c2f0e-0000-4000-8000-000000000001",
  playa_name: "Dusty",
  superuser: false,
  permissions: ["core.modules.read"],
};
const adminBody = { version: "v1", graph: { modules: [], edges: [], warnings: [] }, schema: [] };

describe("a 401 keeps the page recoverable", () => {
  it("401 -> sign in -> lands on returnTo", async () => {
    let signedIn = true;
    let expired = true;
    const { router } = mountApp({
      path: "/admin/modules",
      session: ident,
      routes: {
        "GET /api/accounts/session": () =>
          json(signedIn ? { signed_in: true, ...ident } : { signed_in: false }),
        "GET /api/admin/modules": () =>
          expired ? problem(401, { code: "unauthenticated" }) : json(adminBody),
        "POST /api/accounts/sign-in": () => {
          signedIn = true;
          expired = false;
          return new Response(null, { status: 204 });
        },
      },
    });
    signedIn = false; // the server forgot us; the next session read says so
    await waitFor(() => expect(router()!.state.location.pathname).toBe("/accounts/sign-in"));
    expect(router()!.state.location.search).toBe("?returnTo=%2Fadmin%2Fmodules");
    await userEvent.type(await screen.findByLabelText(/E-mail address/), "pat@example.test");
    await userEvent.type(screen.getByLabelText(/Password/), "correct horse");
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    await waitFor(() => expect(router()!.state.location.pathname).toBe("/admin/modules"));
    expect(await screen.findByText("No modules are compiled into this build.")).toBeInTheDocument();
  });
});

describe("a failed background refetch of /api/modules", () => {
  it("keeps the page instead of replacing it with the boot failure", async () => {
    let fail = false;
    const { client } = mountApp({
      routes: {
        "GET /api/modules": () =>
          fail
            ? problem(503, { code: "unavailable" })
            : json({ modules: [{ name: "hello", enabled: true }] }),
      },
    });
    await screen.findByRole("heading", { name: "Welcome to BMS" });
    fail = true;
    await act(async () => {
      await client.refetchQueries({ queryKey: ["core", "modules"] });
    });
    expect(screen.getByRole("heading", { name: "Welcome to BMS" })).toBeInTheDocument();
    expect(
      screen.queryByRole("heading", { name: "Can't reach the server" }),
    ).not.toBeInTheDocument();
  });
});

describe("the boot failure heading follows the code", () => {
  it("a server error says something went wrong, a network failure says it can't reach the server", async () => {
    const { unmount } = mountApp({
      routes: { "GET /api/modules": () => problem(500, { code: "internal", ref: "AAAA-2222" }) },
    });
    expect(
      await screen.findByRole("heading", { level: 1, name: "Something went wrong" }),
    ).toBeInTheDocument();
    unmount();
    mountApp({ routes: { "GET /api/modules": () => Promise.reject(new TypeError("offline")) } });
    expect(
      await screen.findByRole("heading", { level: 1, name: "Can't reach the server" }),
    ).toBeInTheDocument();
  });
});

describe("a set's catalogue chunk failing to load", () => {
  it("is reported, the page still renders, and a retry succeeds", async () => {
    const sent: Batch[] = [];
    const reporter = installReporter({
      appVersion: "dev",
      send: (b) => {
        sent.push(b);
        return Promise.resolve({ kind: "accepted" });
      },
    });
    let broken = true;
    const flaky: ScreenSet = {
      name: "flaky",
      kind: "module",
      routes: [
        {
          index: true,
          element: <h1>Flaky</h1>,
          handle: { title: "shell.home.title", access: "public" },
        },
      ],
      nav: [],
      messages: {
        en: () => (broken ? Promise.reject(new Error("chunk failed")) : Promise.resolve({})),
      },
    };
    mountApp({ modules: [{ name: "flaky", enabled: true }], sets: [flaky] });
    expect(await screen.findByText("Part of this page couldn't load.")).toBeInTheDocument();
    expect(screen.getByRole("banner")).toBeInTheDocument();
    reporter.flush("manual");
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]!.reports[0]).toMatchObject({ kind: "screen", message: "Error: chunk failed" });
    broken = false;
    await userEvent.click(
      within(screen.getByText("Part of this page couldn't load.").closest("div")!).getByRole(
        "button",
        { name: "Try again" },
      ),
    );
    await waitFor(() =>
      expect(screen.queryByText("Part of this page couldn't load.")).not.toBeInTheDocument(),
    );
  });
});

describe("live regions", () => {
  it("keeps one empty role=status container mounted for the banners", async () => {
    mountApp();
    await screen.findByRole("heading", { level: 1 });
    const hosts = screen
      .getAllByRole("status")
      .filter(
        (s) =>
          s.parentElement?.tagName === "DIV" && s.children.length === 0 && s.textContent === "",
      );
    expect(hosts.length).toBeGreaterThan(0);
  });

  it("rate_limited without Retry-After does not invent a number", async () => {
    setupI18n();
    const { messageFor } = await import("@/ui");
    const { ApiError } = await import("@/api");
    const e = new ApiError({
      status: 429,
      code: "rate_limited",
      method: "POST",
      route: "/api/x",
      module: "x",
      traceId: "0".repeat(32),
    });
    const t = (k: string, v?: Record<string, unknown>) => `${k}:${JSON.stringify(v ?? {})}`;
    expect(messageFor(t as never, e)).toBe("error.rate_limited.generic:{}");
  });
});
