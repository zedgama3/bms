import { screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { mountApp } from "@/test/app";
import { json, problem } from "@/test/fakeFetch";
import { applyPublic, resetPublicForTest, termKey } from "./publicDoc";

afterEach(() => {
  document.getElementById("bms-theme")?.remove();
  document.title = "";
});

const ident = (permissions: string[]) => ({
  person_id: "7d2c2f0e-0000-4000-8000-000000000001",
  account_id: "0b6f1c3e-0000-4000-8000-000000000002",
  playa_name: "Dusty",
  superuser: false,
  password_change_required: false,
  expires_at: "2099-01-01T00:00:00Z",
  signup_open: true,
  permissions,
});

const publicDoc = (over: Record<string, unknown> = {}) => ({
  name: "Dust Bowl",
  contact_email: "",
  language: "en",
  languages: ["en", "es"],
  public_url: "",
  theme: false,
  timezone: "",
  terms: {
    en: {
      term_department: "team",
      term_department_plural: "teams",
      term_department_title: "Team",
      term_department_plural_title: "Teams",
      term_event: "gathering",
      term_event_plural: "gatherings",
      term_event_title: "Gathering",
      term_event_plural_title: "Gatherings",
      term_theme_camp: "camp",
      term_theme_camp_plural: "camps",
      term_theme_camp_title: "Camp",
      term_theme_camp_plural_title: "Camps",
    },
  },
  ...over,
});

describe("the public settings document at boot", () => {
  it("maps a term argument back to its catalogue key", () => {
    expect(termKey("term_department")).toBe("term.department");
    expect(termKey("term_department_plural")).toBe("term.department.plural");
    expect(termKey("term_department_title")).toBe("term.department.title");
    expect(termKey("term_theme_camp_plural_title")).toBe("term.theme_camp.plural_title");
    expect(termKey("greeting")).toBeNull();
  });

  it("shows the regional's name in the header and the footer, and its words in the menu", async () => {
    mountApp({
      session: ident([]),
      routes: {
        "GET /api/settings/public": () => json(publicDoc()),
        "GET /api/people/me": () => problem(404, { code: "not_found" }),
        "GET /api/departments/": () => json({ departments: [] }),
      },
    });
    await screen.findByRole("heading", { name: /Hi, Dusty/ });
    const header = screen.getByRole("banner");
    expect(within(header).getByRole("link", { name: "Dust Bowl" })).toHaveAttribute("href", "/");
    const main = screen.getByRole("navigation", { name: "Main" });
    expect(within(main).getByRole("link", { name: "Teams" })).toBeInTheDocument();
    expect(within(main).getByRole("link", { name: "Gatherings" })).toBeInTheDocument();
    expect(within(main).queryByRole("link", { name: "Departments" })).not.toBeInTheDocument();
  });

  it("uses the terms in screen titles and messages", async () => {
    mountApp({
      path: "/departments",
      session: ident([]),
      routes: {
        "GET /api/settings/public": () => json(publicDoc()),
        "GET /api/people/me": () => problem(404, { code: "not_found" }),
        "GET /api/departments/": () => json({ departments: [] }),
      },
    });
    expect(await screen.findByRole("heading", { level: 1, name: "Teams" })).toBeInTheDocument();
    expect(await screen.findByText("No team has been created yet.")).toBeInTheDocument();
  });

  it("links the theme stylesheet only when the regional has one", async () => {
    const first = mountApp({
      routes: { "GET /api/settings/public": () => json(publicDoc({ theme: true })) },
    });
    await screen.findByRole("heading", { level: 1 });
    const link = document.getElementById("bms-theme");
    expect(link).toBeInstanceOf(HTMLLinkElement);
    expect(link).toHaveAttribute("rel", "stylesheet");
    expect(link?.getAttribute("href")).toMatch(/^\/api\/settings\/theme\.css\?v=\d+$/);
    first.unmount();
    document.getElementById("bms-theme")?.remove();
    mountApp({ routes: { "GET /api/settings/public": () => json(publicDoc()) } });
    await screen.findByRole("heading", { level: 1 });
    expect(document.getElementById("bms-theme")).toBeNull();
  });

  it("keeps the shipped defaults, and still boots, when the document cannot be had", async () => {
    mountApp({
      routes: {
        "GET /api/settings/public": () => problem(503, { code: "unavailable", ref: "AAAA-0009" }),
      },
    });
    expect(
      await screen.findByRole("heading", { level: 1, name: "Welcome to BMS" }),
    ).toBeInTheDocument();
    expect(
      within(screen.getByRole("banner")).getByRole("link", { name: "BMS" }),
    ).toBeInTheDocument();
  });

  it("puts the name in the page title", async () => {
    mountApp({
      path: "/departments",
      session: ident([]),
      routes: {
        "GET /api/settings/public": () => json(publicDoc()),
        "GET /api/people/me": () => problem(404, { code: "not_found" }),
        "GET /api/departments/": () => json({ departments: [] }),
      },
    });
    await screen.findByRole("heading", { level: 1, name: "Teams" });
    expect(document.title).toBe("Teams · Dust Bowl");
  });

  it("re-links the theme only when the colours may have changed", () => {
    resetPublicForTest();
    const doc = publicDoc({ theme: true }) as Parameters<typeof applyPublic>[0];
    const href = () => document.getElementById("bms-theme")?.getAttribute("href");
    applyPublic(doc);
    const first = href();
    expect(first).toMatch(/theme\.css\?v=\d+$/);
    applyPublic(doc); // a plain refetch
    expect(href()).toBe(first);
    applyPublic(doc, true); // after a save
    expect(href()).not.toBe(first);
    applyPublic({ ...doc, theme: false });
    expect(document.getElementById("bms-theme")).toBeNull();
  });

  it("goes on booting after the first failure while the retries continue", async () => {
    let calls = 0;
    mountApp({
      routes: {
        "GET /api/settings/public": () => {
          calls++;
          return problem(503, { code: "unavailable" });
        },
      },
    });
    expect(await screen.findByRole("heading", { level: 1 })).toBeInTheDocument();
    expect(calls).toBeGreaterThan(0);
  });
});
