import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { expectNoAxeViolations } from "@/test/axe";
import { mountApp } from "@/test/app";
import { json, problem } from "@/test/fakeFetch";

const person = {
  person_id: "7d2c2f0e-0000-4000-8000-000000000001",
  playa_name: "Dusty",
  superuser: false,
  permissions: [],
};

describe("hello (the reference module)", () => {
  it("signed out: the public greeting and its banners only, with no sign-in prompt of its own", async () => {
    const { container, fetchMock } = mountApp({ path: "/hello" });
    expect(await screen.findByRole("heading", { level: 1, name: "Hello!" })).toBeInTheDocument();
    expect(await screen.findByText("Welcome to the reference module.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Say hello" })).not.toBeInTheDocument();
    expect(
      fetchMock.mock.calls.some(([r]) => new URL(r.url).pathname === "/api/hello/greeting"),
    ).toBe(false);
    await expectNoAxeViolations(container);
  });

  it("drops a banner whose key is not in a loaded catalogue", async () => {
    mountApp({
      path: "/hello",
      routes: {
        "GET /api/hello/public-greeting": () =>
          json({
            greeting: "Hi",
            banners: [
              { module: "x", key: "module.x.banner.unknown" },
              { module: "hello", key: "module.hello.banner.welcome" },
            ],
          }),
      },
    });
    expect(await screen.findByText("Welcome to the reference module.")).toBeInTheDocument();
    expect(screen.queryByText(/module\.x\.banner/)).not.toBeInTheDocument();
    expect(within(screen.getByRole("main")).getAllByRole("listitem")).toHaveLength(1);
  });

  it("public error: ErrorState with retry (network has no reference)", async () => {
    mountApp({
      path: "/hello",
      routes: { "GET /api/hello/public-greeting": () => Promise.reject(new TypeError("offline")) },
    });
    expect(await screen.findByRole("alert")).toHaveTextContent("Can't reach the server.");
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument();
  });

  it("signed in: counts, a button, and a greeting refetches both queries", async () => {
    let mine = 0;
    const { container } = mountApp({
      path: "/hello",
      session: person,
      routes: {
        "GET /api/hello/greeting": () => json({ greeting: "Hello", mine, seen: mine + 4 }),
        "POST /api/hello/greetings": () => {
          mine++;
          return json(
            {
              id: "7d2c2f0e-0000-4000-8000-000000000002",
              greeting: "Hello",
              created_at: "2026-10-17T12:00:00Z",
            },
            { status: 201 },
          );
        },
      },
    });
    expect(await screen.findByText("You have greeted never.")).toBeInTheDocument();
    expect(
      screen.getByText("This server has seen 4 greetings since it started."),
    ).toBeInTheDocument();
    await expectNoAxeViolations(container);
    await userEvent.click(screen.getByRole("button", { name: "Say hello" }));
    expect(await screen.findByText("You have greeted once.")).toBeInTheDocument();
    expect(screen.getAllByText("Greeting saved").length).toBeGreaterThan(0);
  });

  it("forbidden counts: the error state shows the permission sentence; the public part stays", async () => {
    mountApp({
      path: "/hello",
      session: person,
      routes: {
        "GET /api/hello/greeting": () => problem(403, { code: "permission", ref: "CAFE-0001" }),
      },
    });
    expect(await screen.findByText("You don't have permission to do that.")).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 1, name: "Hello!" })).toBeInTheDocument();
    expect(screen.getByText("CAFE-0001")).toBeInTheDocument();
  });

  it("read_only on greet: the summary says so (no reference) and the button is usable again", async () => {
    mountApp({
      path: "/hello",
      session: person,
      routes: {
        "GET /api/hello/greeting": () => json({ greeting: "Hello", mine: 0, seen: 0 }),
        "POST /api/hello/greetings": () =>
          problem(503, { code: "read_only", ref: "CAFE-0002" }, { "X-Read-Only": "true" }),
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: "Say hello" }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("This copy is read-only right now, so that can't be saved.");
    expect(alert).not.toHaveTextContent("CAFE-0002");
    expect(alert).toHaveFocus();
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Say hello" })).not.toHaveAttribute(
        "aria-disabled",
      ),
    );
  });

  it("the button disables while pending: no double submit", async () => {
    let calls = 0;
    mountApp({
      path: "/hello",
      session: person,
      routes: {
        "GET /api/hello/greeting": () => json({ greeting: "Hello", mine: 0, seen: 0 }),
        "POST /api/hello/greetings": () => {
          calls++;
          return new Promise<Response>(() => {});
        },
      },
    });
    const button = await screen.findByRole("button", { name: "Say hello" });
    await userEvent.click(button);
    await waitFor(() => expect(button).toHaveAttribute("aria-disabled", "true"));
    expect(button).not.toBeDisabled(); // aria-disabled keeps focus
    await userEvent.click(button);
    expect(calls).toBe(1);
  });
});
