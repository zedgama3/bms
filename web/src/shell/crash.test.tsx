import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@/api";
import { expectNoAxeViolations } from "@/test/axe";
import { mountApp } from "@/test/app";
import { getReporter, installReporter, uninstallReporterForTest } from "./report";
import type { Batch } from "./report";
import type { ScreenSet } from "@/registry/types";

afterEach(() => {
  uninstallReporterForTest();
});

function throwing(error: unknown): ScreenSet {
  return {
    name: "boom",
    kind: "module",
    routes: [
      {
        path: "x/:id",
        Component: () => {
          throw error;
        },
        handle: { title: "module.hello.title", access: "public" },
      },
    ],
    nav: [],
    messages: { en: () => Promise.resolve({}) },
  };
}

describe("a crashed screen", () => {
  it("shows the crashed state inside the frame, reports it with the route pattern, and offers try again and go home", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    const sent: Batch[] = [];
    installReporter({
      appVersion: "dev",
      send: (b) => {
        sent.push(b);
        return Promise.resolve({ kind: "accepted" });
      },
    });
    const { container } = mountApp({
      modules: [{ name: "boom", enabled: true }],
      sets: [throwing(new Error("kaboom ZQ9MARKER7"))],
      path: "/boom/x/42",
    });
    expect(
      await screen.findByRole("heading", { level: 1, name: "Something went wrong" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Something went wrong on this screen.")).toBeInTheDocument();
    expect(screen.getByRole("banner")).toBeInTheDocument(); // the frame stays
    expect(screen.getByRole("link", { name: "Go home" })).toHaveAttribute("href", "/");
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument();
    await expectNoAxeViolations(container);
    const { getReporter } = await import("./report");
    getReporter()!.flush("manual");
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]!.reports[0]).toMatchObject({ kind: "screen", screen: "/boom/x/{id}" });
  });

  it("an ApiError with a reference shows the reference line", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    const e = new ApiError({
      status: 500,
      code: "internal",
      ref: "FACE-0001",
      method: "GET",
      route: "/api/x",
      module: "x",
      traceId: "0".repeat(31) + "1",
    });
    mountApp({
      modules: [{ name: "boom", enabled: true }],
      sets: [throwing(e)],
      path: "/boom/x/1",
    });
    expect(await screen.findByText("FACE-0001")).toBeInTheDocument();
  });

  it("try again re-renders the route", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    let fail = true;
    const set: ScreenSet = {
      ...throwing(new Error("first")),
      routes: [
        {
          path: "x/:id",
          Component: () => {
            if (fail) throw new Error("again");
            return <h1>Recovered</h1>;
          },
          handle: { title: "module.hello.title", access: "public" },
        },
      ],
    };
    mountApp({ modules: [{ name: "boom", enabled: true }], sets: [set], path: "/boom/x/1" });
    const retry = await screen.findByRole("button", { name: "Try again" });
    fail = false;
    await userEvent.click(retry);
    expect(await screen.findByRole("heading", { name: "Recovered" })).toBeInTheDocument();
  });
});

describe("nothing personal in browser storage", () => {
  it("a full visit writes no localStorage or sessionStorage key", async () => {
    mountApp();
    await screen.findByRole("heading", { level: 1 });
    expect(localStorage.length).toBe(0);
    expect(sessionStorage.length).toBe(0);
    expect(document.cookie).toBe("");
  });
});

describe("a stale bundle (D54, F45)", () => {
  it("says the site was updated and offers Reload", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    const reload = vi.fn();
    vi.stubGlobal("location", { ...window.location, reload });
    const sent: Batch[] = [];
    installReporter({
      appVersion: "dev",
      send: (b) => {
        sent.push(b);
        return Promise.resolve({ kind: "accepted" });
      },
    });
    mountApp({
      modules: [{ name: "boom", enabled: true }],
      sets: [
        throwing(
          new TypeError(
            "Failed to fetch dynamically imported module: https://x.example/assets/boom-1.js",
          ),
        ),
      ],
      path: "/boom/x/1",
    });
    expect(
      await screen.findByRole("heading", { name: "This site was updated" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Reload" }));
    expect(reload).toHaveBeenCalled();
    vi.unstubAllGlobals();
    getReporter()?.flush("manual");
    await waitFor(() =>
      expect(sent[0]!.reports[0]).toMatchObject({
        kind: "chunk",
        message: "chunk load failed assets/boom-1.js",
      }),
    );
  });
});
