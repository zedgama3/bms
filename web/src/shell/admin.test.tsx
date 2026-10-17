import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { expectNoAxeViolations } from "@/test/axe";
import { mountApp } from "@/test/app";
import { json, problem } from "@/test/fakeFetch";

const person = {
  person_id: "7d2c2f0e-0000-4000-8000-000000000001",
  playa_name: "Dusty",
  superuser: false,
  permissions: ["core.modules.read"],
};

// The shape of api.md's AdminModulesResponse (lower-case keys).
const fixture = {
  version: "v0.1.0",
  graph: {
    modules: [
      {
        name: "hello",
        enabled: true,
        has_schema: true,
        permissions: ["hello.greetings.read", "hello.greetings.write"],
        settings: ["hello.greeting"],
        publishes: ["hello.greeted"],
        subscribes: ["hello.greeted"],
        provides: ["greeter"],
        requires: [],
        uses: [],
        defines: ["hello.banner"],
        contributes_to: ["hello.banner"],
      },
      { name: "other", enabled: false, has_schema: false },
    ],
    edges: [{ from: "other", to: "hello", kind: "uses", via: "greeter" }],
    warnings: [{ module: "other", message: "uses hello, which is disabled" }],
  },
  schema: [{ set: "hello", built: 1, applied: 1, current: true }],
};

const at = (routes: Record<string, () => Response | Promise<Response>>) => ({
  path: "/admin/modules",
  session: person,
  routes,
});

describe("/admin/modules", () => {
  it("success: version, a table per module, warnings in English, edges", async () => {
    const { container } = mountApp(at({ "GET /api/admin/modules": () => json(fixture) }));
    expect(await screen.findByRole("heading", { level: 1, name: "Modules" })).toBeInTheDocument();
    expect(await screen.findByText("Version: v0.1.0")).toBeInTheDocument();
    const hello = screen.getByRole("heading", { name: /Hello/, level: 2 });
    expect(hello).toBeInTheDocument();
    const table = screen.getAllByRole("table")[0]!;
    expect(within(table).getByText("Up to date (version 1)")).toBeInTheDocument();
    expect(
      within(table).getByText("hello.greetings.read and hello.greetings.write"),
    ).toBeInTheDocument();
    expect(within(table).getAllByText("None").length).toBeGreaterThan(0);
    const warning = screen.getByText(/other: uses hello, which is disabled/);
    expect(warning).toHaveAttribute("lang", "en");
    const edges = screen.getByRole("table", { name: "Connections" });
    expect(within(edges).getByText("greeter")).toBeInTheDocument();
    await expectNoAxeViolations(container);
  });

  it("a module with a stale schema says it needs bms migrate", async () => {
    const stale = structuredClone(fixture);
    stale.schema = [{ set: "hello", built: 3, applied: 1, current: false }];
    mountApp(at({ "GET /api/admin/modules": () => json(stale) }));
    expect(await screen.findByText("Needs bms migrate (applied 1, built 3)")).toBeInTheDocument();
  });

  it("loading: the h1 is there and the region is busy", async () => {
    mountApp(at({ "GET /api/admin/modules": () => new Promise<Response>(() => {}) }));
    expect(await screen.findByRole("heading", { level: 1, name: "Modules" })).toBeInTheDocument();
    expect(document.querySelector('[aria-busy="true"]')).not.toBeNull();
  });

  it("forbidden: 403 shows the access sentence and the reference", async () => {
    const { container } = mountApp(
      at({
        "GET /api/admin/modules": () => problem(403, { code: "permission", ref: "BEEF-0001" }),
      }),
    );
    expect(await screen.findByText("You don't have access to this screen.")).toBeInTheDocument();
    expect(screen.getByText("BEEF-0001")).toBeInTheDocument();
    await expectNoAxeViolations(container);
  });

  it("error: a 500 shows the error state with the reference and retry", async () => {
    let ok = false;
    mountApp(
      at({
        "GET /api/admin/modules": () =>
          ok ? json(fixture) : problem(500, { code: "internal", ref: "BEEF-0002" }),
      }),
    );
    expect(await screen.findByText("Something went wrong.")).toBeInTheDocument();
    expect(screen.getByText("BEEF-0002")).toBeInTheDocument();
    ok = true;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("Version: v0.1.0")).toBeInTheDocument();
  });

  it("empty: no modules compiled in", async () => {
    mountApp(
      at({
        "GET /api/admin/modules": () =>
          json({ version: "dev", graph: { modules: [], edges: [], warnings: [] }, schema: [] }),
      }),
    );
    expect(await screen.findByText("No modules are compiled into this build.")).toBeInTheDocument();
  });
});
