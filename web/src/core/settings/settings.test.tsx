import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import shellEn from "@/shell/messages/en.json";
import { mountApp } from "@/test/app";
import { expectNoAxeViolations } from "@/test/axe";
import { json, problem } from "@/test/fakeFetch";
import { changeFor, composeDuration, contrastRatio, parseDuration, parseHex } from "./helpers";
import type { SettingDoc } from "./types";

const ME = "7d2c2f0e-0000-4000-8000-000000000001";
const ident = (permissions: string[]) => ({
  person_id: ME,
  account_id: "0b6f1c3e-0000-4000-8000-000000000002",
  playa_name: "Dusty",
  superuser: false,
  password_change_required: false,
  expires_at: "2099-01-01T00:00:00Z",
  signup_open: true,
  permissions,
});
const ADMIN = ["settings.read", "settings.manage"];

const setting = (over: Partial<SettingDoc> & { key: string }): SettingDoc => ({
  kind: "text",
  default: "",
  value: null,
  secret_set: false,
  stale: false,
  ...over,
});

const documentBody = (over: { areas?: unknown[] } = {}) => ({
  revision: 4,
  areas: over.areas ?? [
    {
      name: "regional",
      core: true,
      settings: [
        setting({ key: "regional.name", default: "BMS", value: "Dust Bowl" }),
        setting({ key: "regional.contact_email" }),
        setting({ key: "regional.public_url" }),
        setting({ key: "regional.language", kind: "enum", default: "en", options: ["en", "es"] }),
        setting({ key: "regional.timezone" }),
        setting({ key: "regional.color_primary", value: "#0b5cad" }),
        setting({ key: "regional.color_primary_dark" }),
      ],
    },
    {
      name: "accounts",
      core: true,
      settings: [
        setting({
          key: "accounts.signup",
          kind: "enum",
          default: "open",
          options: ["open", "invitation"],
        }),
        setting({ key: "accounts.session_lifetime", kind: "duration", default: "720h" }),
        setting({ key: "accounts.password_min_length", kind: "int", default: "12", value: "14" }),
        setting({
          key: "accounts.login_failure_window",
          kind: "duration",
          default: "15m",
          value: "1h",
          stale: true,
        }),
      ],
    },
    {
      name: "audit",
      core: true,
      settings: [setting({ key: "audit.retention", kind: "duration", default: "0" })],
    },
    {
      name: "hello",
      core: false,
      settings: [
        setting({ key: "hello.greeting", default: "Hello" }),
        setting({ key: "hello.shout", kind: "bool", default: "false" }),
        setting({ key: "hello.token", kind: "secret", secret_set: true }),
        setting({ key: "hello.other_token", kind: "secret" }),
      ],
    },
  ],
});

type Routes = Record<string, (req: Request) => Response | Promise<Response>>;
const base = (over: Routes = {}): Routes => ({
  "GET /api/settings/": () => json(documentBody()),
  "GET /api/people/me": () => problem(404, { code: "not_found" }),
  "PUT /api/settings/values": () => json({ revision: 5 }),
  ...over,
});

type Calls = { mock: { calls: [Request][] } };
const bodyOf = async (fetchMock: Calls, method: string, path: string) => {
  const hit = fetchMock.mock.calls.find(
    ([r]) => r.method === method && new URL(r.url).pathname === path,
  );
  return hit ? ((await hit[0].clone().json()) as unknown) : undefined;
};
const seen = (fetchMock: Calls, method: string, path: string) =>
  fetchMock.mock.calls.some(([r]) => r.method === method && new URL(r.url).pathname === path);

describe("helpers", () => {
  it("reads a duration as the largest exact unit and writes days as hours", () => {
    expect(parseDuration("720h")).toEqual({ n: "30", unit: "d" });
    expect(parseDuration("15m")).toEqual({ n: "15", unit: "m" });
    expect(parseDuration("1h30m")).toEqual({ n: "90", unit: "m" });
    expect(parseDuration("36h")).toEqual({ n: "36", unit: "h" });
    expect(parseDuration("0")).toEqual({ n: "0", unit: "h" });
    expect(parseDuration("0h")).toEqual({ n: "0", unit: "h" });
    expect(parseDuration("soon")).toEqual({ n: "", unit: "h" });
    expect(composeDuration("30", "d")).toBe("720h");
    expect(composeDuration("15", "m")).toBe("15m");
    expect(composeDuration("", "h")).toBe("");
  });

  it("sends only real changes: a reset of a default is nothing, a typed default is a reset", () => {
    const stored = setting({ key: "a.b", default: "x", value: "y" });
    const plain = setting({ key: "a.b", default: "x" });
    expect(changeFor(stored, undefined)).toBeUndefined();
    expect(changeFor(stored, null)).toBeNull();
    expect(changeFor(plain, null)).toBeUndefined();
    expect(changeFor(stored, "x")).toBeNull();
    expect(changeFor(plain, "x")).toBeUndefined();
    expect(changeFor(stored, " y ")).toBeUndefined();
    expect(changeFor(plain, " z ")).toBe("z");
    expect(changeFor({ ...stored, stale: true }, "y")).toBe("y");
    const secret = setting({ key: "a.s", kind: "secret", secret_set: true });
    expect(changeFor(secret, "")).toBeUndefined();
    expect(changeFor(secret, " pw ")).toBe(" pw ");
    expect(changeFor(secret, null)).toBeNull();
    expect(changeFor({ ...secret, secret_set: false }, null)).toBeUndefined();
  });

  it("computes the WCAG contrast ratio", () => {
    expect(contrastRatio([0, 0, 0], [255, 255, 255])).toBeCloseTo(21, 1);
    expect(parseHex("#0B5CAD")).toEqual([11, 92, 173]);
    expect(parseHex("0b5cad")).toBeNull();
  });
});

describe("navigation", () => {
  it("lists Settings for settings.read and not otherwise", async () => {
    mountApp({ session: ident(ADMIN), routes: base() });
    await screen.findByRole("heading", { name: "Hi, Dusty" });
    const admin = screen.getByRole("navigation", { name: "Administration" });
    expect(within(admin).getByRole("link", { name: "Settings" })).toHaveAttribute(
      "href",
      "/settings",
    );
  });

  it("hides it from a person without the permission", async () => {
    mountApp({ session: ident(["settings.manage"]), routes: base() });
    await screen.findByRole("heading", { name: "Hi, Dusty" });
    expect(screen.queryByRole("link", { name: "Settings" })).not.toBeInTheDocument();
  });
});

describe("/settings (regional)", () => {
  it("draws each field with the right control, input type and mode", async () => {
    const { container } = mountApp({ path: "/settings", session: ident(ADMIN), routes: base() });
    expect(await screen.findByRole("heading", { level: 1, name: "Settings" })).toBeInTheDocument();
    const name = await screen.findByLabelText(/^Name/);
    expect(name).toHaveValue("Dust Bowl");
    expect(name).toHaveAttribute("name", "regional.name");
    const email = screen.getByLabelText("Contact mailbox");
    expect(email).toHaveAttribute("type", "email");
    expect(email).toHaveAttribute("inputmode", "email");
    expect(screen.getByLabelText("Web address")).toHaveAttribute("type", "url");
    const lang = screen.getByLabelText("Default language");
    expect(within(lang).getByRole("option", { name: "English" })).toBeInTheDocument();
    const zone = screen.getByLabelText("Time zone");
    expect(within(zone).getAllByRole("option")[0]).toHaveTextContent("The browser's own time zone");
    expect(screen.getByLabelText("Brand colour")).toHaveValue("#0b5cad");
    expect(screen.getByText(/Contrast 6\.\d to 1 on white: readable/)).toBeInTheDocument();
    expect(screen.getAllByText("No colour is set, so the shipped colour is used.")).toHaveLength(1);
    // links to terminology and the other areas (the regional's own are on this page)
    expect(screen.getByRole("link", { name: "Terminology" })).toHaveAttribute(
      "href",
      "/settings/terms",
    );
    expect(screen.getByRole("link", { name: "Accounts" })).toHaveAttribute(
      "href",
      "/settings/accounts",
    );
    await expectNoAxeViolations(container);
  });

  it("warns live that the colour is too faint, in words", async () => {
    mountApp({ path: "/settings", session: ident(ADMIN), routes: base() });
    const colour = await screen.findByLabelText("Brand colour");
    await userEvent.clear(colour);
    await userEvent.type(colour, "#cccccc");
    expect(
      screen.getByText(/on white: too low \(at least 4\.5 to 1 is needed\)/),
    ).toBeInTheDocument();
  });

  it("saves only the changed keys and shows Saved", async () => {
    const { fetchMock } = mountApp({ path: "/settings", session: ident(ADMIN), routes: base() });
    const name = await screen.findByLabelText(/^Name/);
    await userEvent.clear(name);
    await userEvent.type(name, "Playa Hall");
    await userEvent.selectOptions(screen.getByLabelText("Default language"), "es");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(seen(fetchMock, "PUT", "/api/settings/values")).toBe(true));
    expect(await bodyOf(fetchMock, "PUT", "/api/settings/values")).toEqual({
      values: { "regional.name": "Playa Hall", "regional.language": "es" },
    });
    expect(await screen.findByText("Saved")).toBeInTheDocument();
    // the frame is told to read the public document again
    await waitFor(() =>
      expect(
        fetchMock.mock.calls.filter(
          ([r]) => r.method === "GET" && new URL(r.url).pathname === "/api/settings/public",
        ).length,
      ).toBeGreaterThan(1),
    );
  });

  it("says nothing was saved when nothing changed", async () => {
    const { fetchMock } = mountApp({ path: "/settings", session: ident(ADMIN), routes: base() });
    await screen.findByLabelText(/^Name/);
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("Nothing has changed.")).toBeInTheDocument();
    expect(seen(fetchMock, "PUT", "/api/settings/values")).toBe(false);
  });

  it("puts field errors on their fields, the contrast error on the colour, and keeps the edits", async () => {
    mountApp({
      path: "/settings",
      session: ident(ADMIN),
      routes: base({
        "PUT /api/settings/values": () =>
          problem(400, {
            code: "invalid",
            fields: [
              { field: "regional.name", code: "required", reason: "required" },
              {
                field: "regional.color_primary",
                code: "contrast",
                reason: "too faint",
                params: { ratio: "2.1", min: "4.5" },
              },
            ],
          }),
      }),
    });
    const name = await screen.findByLabelText(/^Name/);
    await userEvent.clear(name);
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    const alert = await screen.findByRole("alert");
    expect(within(alert).getByRole("link", { name: /Name.*Required/ })).toBeInTheDocument();
    expect(
      within(alert).getByRole("link", { name: /Brand colour.*2\.1 to 1.*4\.5 to 1/ }),
    ).toBeInTheDocument();
    expect(name).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByLabelText("Brand colour")).toHaveAttribute("aria-invalid", "true");
    expect(name).toHaveValue("");
  });

  it.each([
    ["scheme", /Must be an http or https web address/],
    ["host", /Must have a valid host name/],
    ["userinfo", /Must not contain a user name or password/],
    ["query", /Must not contain a \? or a #/],
    ["fragment", /Must not contain a \? or a #/],
    ["chars", /Must not contain spaces, control characters or a broken % code/],
    ["newer", /Must be a web address/],
  ])("words the web address's url code for the reason %s", async (reason, words) => {
    mountApp({
      path: "/settings",
      session: ident(ADMIN),
      routes: base({
        "PUT /api/settings/values": () =>
          problem(400, {
            code: "invalid",
            fields: [
              { field: "regional.public_url", code: "url", reason: "x", params: { reason } },
            ],
          }),
      }),
    });
    const input = await screen.findByLabelText(/^Web address/);
    await userEvent.type(input, "x");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    const alert = await screen.findByRole("alert");
    expect(within(alert).getByRole("link", { name: words })).toBeInTheDocument();
    expect(input).toHaveAttribute("aria-invalid", "true");
  });

  it("words a url code that arrives without a reason in general terms", async () => {
    mountApp({
      path: "/settings",
      session: ident(ADMIN),
      routes: base({
        "PUT /api/settings/values": () =>
          problem(400, {
            code: "invalid",
            fields: [{ field: "regional.public_url", code: "url", reason: "x" }],
          }),
      }),
    });
    const input = await screen.findByLabelText(/^Web address/);
    await userEvent.type(input, "x");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    const alert = await screen.findByRole("alert");
    expect(within(alert).getByRole("link", { name: /Must be a web address/ })).toBeInTheDocument();
  });

  it("keeps the contrast result in an always-present status region tied to the input", async () => {
    mountApp({ path: "/settings", session: ident(ADMIN), routes: base() });
    const colour = await screen.findByLabelText("Brand colour", { exact: true });
    const ids = (colour.getAttribute("aria-describedby") ?? "").split(" ");
    const status = document.getElementById(ids[ids.length - 1]!);
    expect(status).toHaveAttribute("role", "status");
    expect(status).toHaveTextContent(/readable/);
    await userEvent.clear(colour);
    // the same region, now saying nothing is set
    expect(document.getElementById(ids[ids.length - 1]!)).toBe(status);
    expect(status).toHaveTextContent("No colour is set");
    const dark = screen.getByLabelText("Brand colour, dark mode", { exact: true });
    expect(dark.getAttribute("aria-describedby")).toMatch(/-status$/);
  });

  it("shows the settings area's own words for a server code it has no shared text for", async () => {
    mountApp({
      path: "/settings",
      session: ident(ADMIN),
      routes: base({
        "PUT /api/settings/values": () =>
          problem(400, {
            code: "invalid",
            fields: [{ field: "regional.name", code: "unknown", reason: "no such key" }],
          }),
      }),
    });
    const name = await screen.findByLabelText(/^Name/);
    await userEvent.type(name, "x");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    const alert = await screen.findByRole("alert");
    expect(
      within(alert).getByRole("link", { name: /This isn't one we know\./ }),
    ).toBeInTheDocument();
  });

  it("disables Save on a read-only copy and says why, keeping the form", async () => {
    mountApp({
      path: "/settings",
      session: ident(ADMIN),
      routes: base(),
      readOnly: () => true,
    });
    await screen.findByLabelText(/^Name/);
    const save = screen.getByRole("button", { name: "Save" });
    await waitFor(() => expect(save).toBeDisabled());
    expect(
      screen.getAllByText("This copy is read-only, so this can't be saved right now.").length,
    ).toBeGreaterThan(0);
  });

  it("shows the forbidden sentence for a 403", async () => {
    mountApp({
      path: "/settings",
      session: ident(ADMIN),
      routes: base({ "GET /api/settings/": () => problem(403, { code: "permission" }) }),
    });
    expect(await screen.findByText("You don't have access to this screen.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Save" })).not.toBeInTheDocument();
  });

  it("shows an error with Try again when the document fails, and recovers", async () => {
    let fail = true;
    mountApp({
      path: "/settings",
      session: ident(ADMIN),
      routes: base({
        "GET /api/settings/": () =>
          fail ? problem(500, { code: "internal", ref: "AAAA-0001" }) : json(documentBody()),
      }),
    });
    const retry = await screen.findByRole("button", { name: "Try again" });
    fail = false;
    await userEvent.click(retry);
    expect(await screen.findByLabelText(/^Name/)).toBeInTheDocument();
  });

  it("explains, without a form, to a person who can manage but not read", async () => {
    const { fetchMock } = mountApp({
      path: "/settings",
      session: ident(["settings.manage"]),
      routes: base(),
    });
    expect(await screen.findByText(/change settings but not read them/)).toBeInTheDocument();
    expect(screen.queryByLabelText(/^Name/)).not.toBeInTheDocument();
    expect(seen(fetchMock, "GET", "/api/settings/")).toBe(false);
  });

  it("is forbidden without either permission", async () => {
    mountApp({ path: "/settings", session: ident([]), routes: base() });
    expect(await screen.findByRole("alert")).toBeInTheDocument();
    expect(screen.queryByLabelText(/^Name/)).not.toBeInTheDocument();
  });

  it("is read-only for a person who can read but not manage", async () => {
    mountApp({ path: "/settings", session: ident(["settings.read"]), routes: base() });
    const name = await screen.findByLabelText(/^Name/);
    expect(name).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Save" })).not.toBeInTheDocument();
    expect(
      screen.getByText("You can read these settings but not change them."),
    ).toBeInTheDocument();
  });
});

describe("/settings/:area", () => {
  it("draws every kind as its control, from the declaring catalogue, with defaults", async () => {
    const { container } = mountApp({
      path: "/settings/accounts",
      session: ident(ADMIN),
      routes: base(),
    });
    expect(await screen.findByRole("heading", { level: 1, name: "Accounts" })).toBeInTheDocument();
    const signup = await screen.findByLabelText("Who can sign up");
    expect(signup.tagName).toBe("SELECT");
    expect(
      within(signup).getByRole("option", { name: "Only people who are invited" }),
    ).toBeInTheDocument();
    // a duration is a number and a unit; 720h reads as 30 days
    const life = screen.getByLabelText("How long people stay signed in");
    expect(life).toHaveValue("30");
    expect(life).toHaveAttribute("inputmode", "numeric");
    expect(screen.getByLabelText("Unit for How long people stay signed in")).toHaveValue("d");
    expect(screen.getByText("Default: 30 days")).toBeInTheDocument();
    expect(screen.getByLabelText("Shortest password")).toHaveValue("14");
    expect(screen.getByText("Default: Anyone can sign up")).toBeInTheDocument();
    await expectNoAxeViolations(container);
  });

  it("shows a zero duration as a zero with its own wording, not a blank", async () => {
    mountApp({ path: "/settings/audit", session: ident(ADMIN), routes: base() });
    const field = await screen.findByLabelText("How long the audit log is kept", { exact: true });
    expect(field).toHaveValue("0");
    expect(screen.getByText("Default: Kept forever")).toBeInTheDocument();
  });

  it("flags a stale value, shows the default, and a save replaces it", async () => {
    const { fetchMock } = mountApp({
      path: "/settings/accounts",
      session: ident(ADMIN),
      routes: base(),
    });
    const window = await screen.findByLabelText("How long failed sign-ins are counted");
    expect(window).toHaveValue("15");
    expect(screen.getByLabelText("Unit for How long failed sign-ins are counted")).toHaveValue("m");
    expect(screen.getByText(/saved value is no longer valid/)).toBeInTheDocument();
    await userEvent.clear(window);
    await userEvent.type(window, "30");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(seen(fetchMock, "PUT", "/api/settings/values")).toBe(true));
    expect(await bodyOf(fetchMock, "PUT", "/api/settings/values")).toEqual({
      values: { "accounts.login_failure_window": "30m" },
    });
  });

  it("converts days to hours and resets a stored value to its default", async () => {
    const { fetchMock } = mountApp({
      path: "/settings/accounts",
      session: ident(ADMIN),
      routes: base(),
    });
    const life = await screen.findByLabelText("How long people stay signed in");
    await userEvent.clear(life);
    await userEvent.type(life, "7");
    await userEvent.click(
      screen.getByRole("button", { name: "Reset to default: Shortest password" }),
    );
    expect(screen.getByLabelText("Shortest password")).toHaveValue("12");
    expect(screen.getByText("Goes back to the default when saved.")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(seen(fetchMock, "PUT", "/api/settings/values")).toBe(true));
    expect(await bodyOf(fetchMock, "PUT", "/api/settings/values")).toEqual({
      values: { "accounts.session_lifetime": "168h", "accounts.password_min_length": null },
    });
  });

  it("never shows a secret: set or unset, Replace types a new one, Clear sends null", async () => {
    const { fetchMock, container } = mountApp({
      path: "/settings/hello",
      session: ident(ADMIN),
      routes: base(),
    });
    // a module's setting without a catalogue entry shows its key
    expect(await screen.findByText("hello.token")).toBeInTheDocument();
    expect(screen.getByText("A value is set. It can't be shown.")).toBeInTheDocument();
    expect(screen.getByText("No value is set.")).toBeInTheDocument();
    expect(container.querySelector('input[type="password"]')).toBeNull();
    // a bool is a checkbox
    expect(screen.getByRole("checkbox", { name: "hello.shout" })).not.toBeChecked();

    await userEvent.click(screen.getByRole("button", { name: "Replace: hello.other_token" }));
    const pw = await screen.findByLabelText("New value for hello.other_token");
    expect(pw).toHaveAttribute("type", "password");
    expect(pw).toHaveAttribute("autocomplete", "new-password");
    expect(pw).toHaveAttribute("name", "hello.other_token");
    await userEvent.type(pw, "s3cret-value");
    await userEvent.click(screen.getByRole("button", { name: "Clear: hello.token" }));
    expect(screen.getByText("Will be cleared when saved.")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("checkbox", { name: "hello.shout" }));
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(seen(fetchMock, "PUT", "/api/settings/values")).toBe(true));
    expect(await bodyOf(fetchMock, "PUT", "/api/settings/values")).toEqual({
      values: {
        "hello.other_token": "s3cret-value",
        "hello.token": null,
        "hello.shout": "true",
      },
    });
    expect(document.body.textContent).not.toContain("s3cret-value");
  });

  it("ties a bool's error to its checkbox", async () => {
    mountApp({
      path: "/settings/hello",
      session: ident(ADMIN),
      routes: base({
        "PUT /api/settings/values": () =>
          problem(400, {
            code: "invalid",
            fields: [{ field: "hello.shout", code: "malformed", reason: "x" }],
          }),
      }),
    });
    const box = await screen.findByRole("checkbox", { name: "hello.shout" });
    await userEvent.click(box);
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(box).toHaveAttribute("aria-invalid", "true"));
    const described = (box.getAttribute("aria-describedby") ?? "").split(" ");
    expect(
      described.some((id) => document.getElementById(id)?.textContent?.includes("expected form")),
    ).toBe(true);
  });

  it("describes the replacement field by its help text", async () => {
    mountApp({ path: "/settings/hello", session: ident(ADMIN), routes: base() });
    await userEvent.click(await screen.findByRole("button", { name: "Replace: hello.token" }));
    expect(await screen.findByLabelText("New value for hello.token")).toHaveAccessibleDescription(
      /never shown again/,
    );
  });

  it("an empty replacement sends nothing, and Cancel goes back", async () => {
    const { fetchMock } = mountApp({
      path: "/settings/hello",
      session: ident(ADMIN),
      routes: base(),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Replace: hello.token" }));
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("Nothing has changed.")).toBeInTheDocument();
    expect(seen(fetchMock, "PUT", "/api/settings/values")).toBe(false);
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByLabelText("New value for hello.token")).not.toBeInTheDocument();
  });

  it("an unsealable deployment's 501 and a conflict are shown in the summary", async () => {
    mountApp({
      path: "/settings/hello",
      session: ident(ADMIN),
      routes: base({
        "PUT /api/settings/values": () => problem(501, { code: "unsupported", ref: "AAAA-0002" }),
      }),
    });
    await userEvent.click(
      await screen.findByRole("button", { name: "Replace: hello.other_token" }),
    );
    await userEvent.type(await screen.findByLabelText("New value for hello.other_token"), "x");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByRole("alert")).toBeInTheDocument();
    // the typed value is still there
    expect(screen.getByLabelText("New value for hello.other_token")).toHaveValue("x");
  });

  it("answers an unknown area with not found, and sends regional back to /settings", async () => {
    const first = mountApp({ path: "/settings/nope", session: ident(ADMIN), routes: base() });
    expect(await screen.findByText("There are no settings with that name.")).toBeInTheDocument();
    first.unmount();
    const second = mountApp({ path: "/settings/regional", session: ident(ADMIN), routes: base() });
    await screen.findByLabelText(/^Name/);
    expect(second.router()!.state.location.pathname).toBe("/settings");
  });
});

const termsBody = () => ({
  languages: ["en", "es"],
  terms: {
    en: {
      department: {
        default: {
          singular: "department",
          plural: "departments",
          singular_title: "Department",
          plural_title: "Departments",
        },
        override: { plural: "teams" },
      },
      event: {
        default: {
          singular: "event",
          plural: "events",
          singular_title: "Event",
          plural_title: "Events",
        },
        override: {},
      },
    },
    es: {
      event: {
        default: {
          singular: "event",
          plural: "events",
          singular_title: "Event",
          plural_title: "Events",
        },
        override: {},
      },
    },
  },
});

describe("/settings/terms", () => {
  const routes = (over: Routes = {}) =>
    base({
      "GET /api/settings/terms": () => json(termsBody()),
      "PUT /api/settings/terms/en": () => json({ revision: 6 }),
      ...over,
    });

  it("shows four fields per term with the default under each", async () => {
    const { container } = mountApp({
      path: "/settings/terms",
      session: ident(ADMIN),
      routes: routes(),
    });
    expect(
      await screen.findByRole("heading", { level: 1, name: "Terminology" }),
    ).toBeInTheDocument();
    const group = await screen.findByRole("group", { name: "Department" });
    expect(within(group).getByLabelText("Several, in a sentence")).toHaveValue("teams");
    expect(within(group).getByLabelText("One, in a sentence")).toHaveValue("");
    expect(within(group).getByText("Default: Departments")).toBeInTheDocument();
    await expectNoAxeViolations(container);
  });

  it("saves one language: sets what changed and resets what was cleared", async () => {
    const { fetchMock } = mountApp({
      path: "/settings/terms",
      session: ident(ADMIN),
      routes: routes(),
    });
    const group = await screen.findByRole("group", { name: "Department" });
    await userEvent.type(within(group).getByLabelText("One, in a sentence"), "team");
    await userEvent.click(
      within(group).getByRole("button", { name: "Reset: Department, Several, in a sentence" }),
    );
    expect(within(group).getByLabelText("Several, in a sentence")).toHaveValue("");
    await userEvent.click(screen.getByRole("button", { name: "Save English" }));
    await waitFor(() => expect(seen(fetchMock, "PUT", "/api/settings/terms/en")).toBe(true));
    expect(await bodyOf(fetchMock, "PUT", "/api/settings/terms/en")).toEqual({
      terms: { department: { singular: "team", reset: ["plural"] } },
    });
    expect(await screen.findByText("Saved")).toBeInTheDocument();
  });

  it("switches language and saves that one", async () => {
    const { fetchMock } = mountApp({
      path: "/settings/terms",
      session: ident(ADMIN),
      routes: routes({ "PUT /api/settings/terms/es": () => json({ revision: 7 }) }),
    });
    await userEvent.selectOptions(await screen.findByLabelText("Language"), "es");
    expect(screen.queryByRole("group", { name: "Department" })).not.toBeInTheDocument();
    const group = screen.getByRole("group", { name: "Event" });
    await userEvent.type(within(group).getByLabelText("Several, as a title"), "Fiestas");
    await userEvent.click(screen.getByRole("button", { name: /^Save/ }));
    await waitFor(() => expect(seen(fetchMock, "PUT", "/api/settings/terms/es")).toBe(true));
    expect(await bodyOf(fetchMock, "PUT", "/api/settings/terms/es")).toEqual({
      terms: { event: { plural_title: "Fiestas" } },
    });
  });

  it("keeps unsaved edits per language when switching", async () => {
    mountApp({ path: "/settings/terms", session: ident(ADMIN), routes: routes() });
    const select = await screen.findByLabelText("Language");
    const en = await screen.findByRole("group", { name: "Event" });
    await userEvent.type(within(en).getByLabelText("One, in a sentence"), "gathering");
    await userEvent.selectOptions(select, "es");
    await userEvent.selectOptions(select, "en");
    const back = await screen.findByRole("group", { name: "Event" });
    expect(within(back).getByLabelText("One, in a sentence")).toHaveValue("gathering");
  });

  it("puts a field error on its box", async () => {
    mountApp({
      path: "/settings/terms",
      session: ident(ADMIN),
      routes: routes({
        "PUT /api/settings/terms/en": () =>
          problem(400, {
            code: "invalid",
            fields: [
              {
                field: "terms.event.singular",
                code: "too_long",
                reason: "long",
                params: { max: "60" },
              },
            ],
          }),
      }),
    });
    const group = await screen.findByRole("group", { name: "Event" });
    const box = within(group).getByLabelText("One, in a sentence");
    await userEvent.type(box, "x");
    await userEvent.click(screen.getByRole("button", { name: "Save English" }));
    await waitFor(() => expect(box).toHaveAttribute("aria-invalid", "true"));
    expect(box).toHaveValue("x");
  });

  it("shows an error state when the terms fail to load", async () => {
    mountApp({
      path: "/settings/terms",
      session: ident(ADMIN),
      routes: routes({
        "GET /api/settings/terms": () => problem(500, { code: "internal", ref: "AAAA-0003" }),
      }),
    });
    expect(await screen.findByRole("button", { name: "Try again" })).toBeInTheDocument();
  });
});

describe("the shell's term entries", () => {
  it("equal the defaults in internal/core/settings/terms/en.json", () => {
    const defaults = JSON.parse(
      readFileSync(resolve(process.cwd(), "../internal/core/settings/terms/en.json"), "utf8"),
    ) as Record<string, Record<string, string>>;
    const want: Record<string, string> = {};
    for (const [term, forms] of Object.entries(defaults)) {
      want[`term.${term}`] = forms["singular"]!;
      want[`term.${term}.plural`] = forms["plural"]!;
      want[`term.${term}.title`] = forms["singular_title"]!;
      want[`term.${term}.plural_title`] = forms["plural_title"]!;
    }
    const have = Object.fromEntries(Object.entries(shellEn).filter(([k]) => k.startsWith("term.")));
    expect(have).toEqual(want);
  });
});
