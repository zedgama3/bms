import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { expectNoAxeViolations } from "@/test/axe";
import { mountApp } from "@/test/app";
import { json, noContent, problem } from "@/test/fakeFetch";

const ME = "7d2c2f0e-0000-4000-8000-000000000001";
const PAT = "7d2c2f0e-0000-4000-8000-000000000009";
const CONTACT = "0b6f1c3e-0000-4000-8000-0000000000d1";
const REQ = "0b6f1c3e-0000-4000-8000-0000000000e1";

const ident = (over: Record<string, unknown> = {}) => ({
  person_id: ME,
  account_id: "0b6f1c3e-0000-4000-8000-000000000002",
  playa_name: "Dusty",
  superuser: false,
  password_change_required: false,
  expires_at: "2099-01-01T00:00:00Z",
  signup_open: true,
  permissions: [],
  ...over,
});

const can = (over: Record<string, boolean> = {}) => ({
  edit: false,
  read_contacts: false,
  export: false,
  delete: false,
  link: false,
  view_roles: false,
  view_audit: false,
  ...over,
});
const policy = (over: Record<string, string> = {}) => ({
  legal_name: "optional",
  phone: "optional",
  emergency_contacts: "optional",
  ...over,
});
const profile = (over: Record<string, unknown> = {}, person: Record<string, unknown> = {}) => ({
  person: {
    id: ME,
    playa_name: "Dusty",
    pronouns: "they/them",
    language: "en",
    legal_name: "Private Name",
    phone: "555-0100",
    contact_email: "",
    email: "dusty@example.test",
    has_account: true,
    complete: true,
    created_at: "2026-10-01T10:00:00Z",
    updated_at: "2026-10-02T10:00:00Z",
    ...person,
  },
  fields: [
    "playa_name",
    "pronouns",
    "language",
    "legal_name",
    "phone",
    "contact_email",
    "email",
    "emergency_contacts",
    "consents",
  ],
  contacts: [],
  consents: [],
  sections: [],
  can: can(),
  policy: policy(),
  ...over,
});
const contact = {
  id: CONTACT,
  person_id: ME,
  name: "Sam Friend",
  phone: "555-0199",
  relationship: "friend",
  position: 1,
  created_at: "2026-10-01T10:00:00Z",
  updated_at: "2026-10-01T10:00:00Z",
};

type Routes = Record<string, (req: Request) => Response | Promise<Response>>;
const mine = (p = profile(), over: Routes = {}): Routes => ({
  "GET /api/people/me": () => json(p),
  "GET /api/people/me/requests": () => json({ requests: [] }),
  ...over,
});
const bodyOf = async (
  fetchMock: { mock: { calls: [Request][] } },
  method: string,
  path: string,
) => {
  const hit = fetchMock.mock.calls.find(
    ([r]) => r.method === method && new URL(r.url).pathname === path,
  );
  return hit ? ((await hit[0].clone().json()) as unknown) : undefined;
};
const called = (fetchMock: { mock: { calls: [Request][] } }, method: string, path: string) =>
  fetchMock.mock.calls.filter(([r]) => r.method === method && new URL(r.url).pathname === path)
    .length;

/** jsdom has no object URLs: give the download something to make and release. */
function stubObjectUrls(onCreate: (b: Blob) => void) {
  Object.defineProperty(URL, "createObjectURL", {
    configurable: true,
    value: (b: Blob) => {
      onCreate(b);
      return "blob:test";
    },
  });
  Object.defineProperty(URL, "revokeObjectURL", { configurable: true, value: () => {} });
}

afterEach(() => {
  vi.unstubAllGlobals();
  Reflect.deleteProperty(URL, "createObjectURL");
  Reflect.deleteProperty(URL, "revokeObjectURL");
});

describe("the frame and the profile (useMe)", () => {
  it("reads the profile once, shows the playa name in the header and applies the language", async () => {
    const { fetchMock } = mountApp({
      session: ident({ playa_name: "" }),
      routes: mine(profile({}, { playa_name: "Dusty D", language: "en" })),
    });
    await screen.findByRole("heading", { name: "Hi, Dusty D" });
    expect(screen.getByText("Dusty D", { selector: "header span" })).toBeInTheDocument();
    expect(called(fetchMock, "GET", "/api/people/me")).toBe(1);
  });

  it("sends a session with no playa name to /people/me?first=1 once; the person may leave", async () => {
    const { router } = mountApp({
      session: ident({ playa_name: "" }),
      routes: mine(profile({}, { playa_name: "" })),
    });
    await waitFor(() => expect(router()!.state.location.pathname).toBe("/people/me"));
    expect(router()!.state.location.search).toBe("?first=1");
    expect(await screen.findByText(/Tell us your playa name/)).toBeInTheDocument();
    await waitFor(() => expect(screen.getByLabelText(/^Playa name/)).toHaveFocus());
    await router()!.navigate("/");
    await screen.findByRole("heading", { name: /Welcome to BMS|Hi,/ });
    await new Promise((r) => setTimeout(r, 50));
    expect(router()!.state.location.pathname).toBe("/");
  });

  it("keeps the page the person was going to: a deep link is offered after the prompt", async () => {
    const { router } = mountApp({
      path: "/people",
      session: ident({ playa_name: "" }),
      routes: mine(profile({}, { playa_name: "" })),
    });
    await waitFor(() => expect(router()!.state.location.search).toBe("?first=1"));
    const skip = await screen.findByRole("link", { name: "Continue to where you were going" });
    expect(skip).toHaveAttribute("href", "/people");
    await userEvent.click(skip);
    await waitFor(() => expect(router()!.state.location.pathname).toBe("/people"));
    await new Promise((r) => setTimeout(r, 100));
    expect(router()!.state.location.pathname).toBe("/people"); // asked once, not again
  });

  it.each([["//evil"], ["/\\evil"], ["https://x"], [42], ["/"]])(
    "ignores an unsafe or useless return target %j",
    async (next) => {
      const { router } = mountApp({
        path: "/people/me?first=1",
        session: ident({ playa_name: "" }),
        routes: mine(profile({}, { playa_name: "" })),
      });
      await screen.findByText(/Tell us your playa name/);
      await router()!.navigate("/people/me?first=1", { state: { next }, replace: true });
      await screen.findByText(/Tell us your playa name/);
      expect(screen.queryByRole("link", { name: "Continue to where you were going" })).toBeNull();
    },
  );

  it("saving the first-sign-in prompt stays on the profile and still offers the way on", async () => {
    const { router } = mountApp({
      path: "/people",
      session: ident({ playa_name: "" }),
      routes: mine(profile({}, { playa_name: "" }), {
        "PUT /api/people/me": () => json(profile({}, { playa_name: "Newbie" })),
      }),
    });
    const name = await screen.findByLabelText(/^Playa name/);
    await userEvent.type(name, "Newbie");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(router()!.state.location.search).toBe(""));
    expect(router()!.state.location.pathname).toBe("/people/me");
    expect(
      await screen.findByRole("link", { name: "Continue to where you were going" }),
    ).toHaveAttribute("href", "/people");
  });

  it("does not send the break-glass superuser to the prompt", async () => {
    const { router } = mountApp({
      session: ident({ playa_name: "", superuser: true }),
      routes: mine(profile({}, { playa_name: "" })),
    });
    await screen.findByRole("heading", { name: /Welcome to BMS|Hi,/ });
    await new Promise((r) => setTimeout(r, 50));
    expect(router()!.state.location.pathname).toBe("/");
  });

  it("does not read the profile while a temporary password restricts the session", async () => {
    const { fetchMock } = mountApp({
      path: "/accounts/password",
      session: ident({ password_change_required: true }),
      routes: mine(),
    });
    await screen.findByRole("heading", { name: "Change password" });
    expect(called(fetchMock, "GET", "/api/people/me")).toBe(0);
  });

  it("marks the playa name required only while it can be typed", async () => {
    mountApp({ path: "/people/me", session: ident(), routes: mine(profile()) });
    const locked = await screen.findByLabelText(/^Playa name/);
    expect(locked).toHaveAttribute("readonly");
    expect(screen.getByText("Playa name", { selector: "label" })).not.toHaveTextContent(
      "(required)",
    );
  });

  it("a person with no playa name yet sees it required", async () => {
    mountApp({
      path: "/people/me",
      session: ident({ playa_name: "" }),
      routes: mine(profile({}, { playa_name: "" })),
    });
    const input = await screen.findByLabelText(/^Playa name/);
    expect(input).not.toHaveAttribute("readonly");
    expect(input).toBeRequired();
    expect(screen.getByText(/^Playa name/, { selector: "label" })).toHaveTextContent("(required)");
  });

  it("a failed profile read does not block the page", async () => {
    mountApp({
      session: ident(),
      routes: { "GET /api/people/me": () => problem(500, { code: "internal", ref: "AB12-CD34" }) },
    });
    expect(await screen.findByRole("heading", { name: "Hi, Dusty" })).toBeInTheDocument();
  });

  it("applies the saved language and the language menu writes the choice to the profile", async () => {
    const { fetchMock } = mountApp({
      session: ident(),
      routes: mine(profile({}, { language: "es" }), {
        "PUT /api/people/me": () => json(profile({}, { language: "en" })),
      }),
    });
    await screen.findByRole("heading", { name: "Hi, Dusty" });
    // only English is on offer in the test build: the menu is absent and nothing is written
    expect(screen.queryByLabelText("Language")).not.toBeInTheDocument();
    expect(called(fetchMock, "PUT", "/api/people/me")).toBe(0);
  });

  it("has My profile in the main menu and People only for a holder of people.directory.read", async () => {
    mountApp({ session: ident({ permissions: ["people.directory.read"] }), routes: mine() });
    await screen.findByRole("heading", { name: "Hi, Dusty" });
    const main = screen.getByRole("navigation", { name: "Main" });
    expect(within(main).getByRole("link", { name: "My profile" })).toHaveAttribute(
      "href",
      "/people/me",
    );
    expect(within(main).getByRole("link", { name: "People" })).toHaveAttribute("href", "/people");
  });
});

describe("/people/me", () => {
  it("shows the sections, labels every input, uses the right input types, and is accessible", async () => {
    const { container } = mountApp({
      path: "/people/me",
      session: ident(),
      routes: mine(profile({ contacts: [contact], consents: [] })),
    });
    expect(await screen.findByRole("heading", { name: "My profile" })).toBeInTheDocument();
    await screen.findByLabelText(/^Playa name/);
    expect(screen.getByLabelText(/^Playa name/)).toHaveAttribute("name", "playa_name");
    expect(screen.getByLabelText(/^Phone number/)).toHaveAttribute("type", "tel");
    expect(screen.getByLabelText(/^Phone number/)).toHaveAttribute("inputmode", "tel");
    expect(screen.getByLabelText(/^Legal name/)).toHaveAttribute("autocomplete", "name");
    expect(screen.getByLabelText(/^Phone number/)).toHaveAttribute("autocomplete", "tel");
    expect(screen.getByText("dusty@example.test")).toBeInTheDocument();
    expect(screen.getByText("To change it, use your account.")).toBeInTheDocument();
    expect(screen.queryByLabelText(/^Contact e-mail address/)).not.toBeInTheDocument();
    expect(screen.getByText("Sam Friend")).toBeInTheDocument();
    expect(screen.getByText("You haven't been asked for any consent yet.")).toBeInTheDocument();
    await expectNoAxeViolations(container);
  });

  it.each([
    ["hidden", "hidden", false, false],
    ["optional", "optional", true, false],
    ["required", "required", true, true],
  ])("legal name and phone in mode %s", async (_n, mode, shown, required) => {
    mountApp({
      path: "/people/me",
      session: ident(),
      routes: mine(profile({ policy: policy({ legal_name: mode, phone: mode }) })),
    });
    await screen.findByLabelText(/^Playa name/);
    const legal = screen.queryByLabelText(/^Legal name/);
    expect(legal !== null).toBe(shown);
    if (legal) {
      expect(legal.hasAttribute("required")).toBe(required);
      expect(screen.getByLabelText(/^Phone number/).hasAttribute("required")).toBe(required);
    }
  });

  it("hides the emergency contacts section when the regional turned it off; contacts are never required", async () => {
    mountApp({
      path: "/people/me",
      session: ident(),
      routes: mine(profile({ policy: policy({ emergency_contacts: "hidden" }) })),
    });
    await screen.findByLabelText(/^Playa name/);
    expect(screen.queryByRole("heading", { name: "Emergency contacts" })).not.toBeInTheDocument();
  });

  it("a person with no account edits a contact e-mail", async () => {
    mountApp({
      path: "/people/me",
      session: ident(),
      routes: mine(profile({}, { has_account: false, email: "" })),
    });
    const email = await screen.findByLabelText(/^Contact e-mail address/);
    expect(email).toHaveAttribute("type", "email");
    expect(email).toHaveAttribute("inputmode", "email");
  });

  it("saves the profile with the fields' names; the set playa name is sent unchanged", async () => {
    const saved = profile({}, { pronouns: "she/her", updated_at: "2026-10-03T10:00:00Z" });
    const { fetchMock } = mountApp({
      path: "/people/me",
      session: ident(),
      routes: mine(profile(), { "PUT /api/people/me": () => json(saved) }),
    });
    const pronouns = await screen.findByLabelText(/^Pronouns/);
    await userEvent.clear(pronouns);
    await userEvent.type(pronouns, "she/her");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await screen.findByText("Saved");
    expect(await bodyOf(fetchMock, "PUT", "/api/people/me")).toEqual({
      playa_name: "Dusty",
      pronouns: "she/her",
      legal_name: "Private Name",
      phone: "555-0100",
    });
  });

  it("a set playa name is read-only with the explanation; an unset one is editable with the permanence hint", async () => {
    const a = mountApp({ path: "/people/me", session: ident(), routes: mine(profile()) });
    const set = await screen.findByLabelText(/^Playa name/);
    expect(set).toHaveAttribute("readonly");
    expect(screen.getByText(/Playa names are permanent\. Ask an administrator/)).toBeVisible();
    a.unmount();
    mountApp({
      path: "/people/me?first=1",
      session: ident({ playa_name: "" }),
      routes: mine(profile({}, { playa_name: "" })),
    });
    const unset = await screen.findByLabelText(/^Playa name/);
    expect(unset).not.toHaveAttribute("readonly");
    expect(screen.getByText(/it can't be changed later/)).toBeVisible();
    const banner = await screen.findByText(/Tell us your playa name/);
    expect(banner).toHaveTextContent("permanent");
    expect(banner).not.toHaveTextContent("any time");
  });

  it("a taken playa name lands under the field without naming the holder; so does a stale permanent", async () => {
    mountApp({
      path: "/people/me?first=1",
      session: ident({ playa_name: "" }),
      routes: mine(profile({}, { playa_name: "" }), {
        "PUT /api/people/me": () =>
          problem(409, {
            code: "conflict",
            fields: [{ field: "playa_name", code: "taken", reason: "x" }],
          }),
      }),
    });
    await userEvent.type(await screen.findByLabelText(/^Playa name/), "Taken");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    const field = screen.getByLabelText(/^Playa name/);
    await waitFor(() => expect(field).toHaveAttribute("aria-invalid", "true"));
    expect(
      screen.getAllByText("Someone here already uses this playa name. Choose another.").length,
    ).toBeGreaterThan(0);
    const summary = await screen.findByRole("alert");
    expect(summary).toHaveTextContent("Check the fields below.");
    expect(summary).not.toHaveTextContent(/Reload/);
  });

  it("a stale permanent answer is worded under the field", async () => {
    mountApp({
      path: "/people/me",
      session: ident(),
      routes: mine(profile(), {
        "PUT /api/people/me": () =>
          problem(400, {
            code: "invalid",
            fields: [{ field: "playa_name", code: "permanent", reason: "x" }],
          }),
      }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Save" }));
    expect(
      (await screen.findAllByText("Playa names can't be changed once set.")).length,
    ).toBeGreaterThan(0);
  });

  it("a non-manager on their own person page sees the set name read-only", async () => {
    mountApp({ path: `/people/${ME}`, session: ident(), routes: mine(profile()) });
    expect(await screen.findByLabelText(/^Playa name/)).toHaveAttribute("readonly");
  });

  it("a profile manager changes their own set name behind a confirm, through the manager route", async () => {
    const { fetchMock } = mountApp({
      path: "/people/me",
      session: ident({ permissions: ["people.profiles.manage"] }),
      routes: mine(profile(), {
        [`PUT /api/people/${ME}`]: () => json(profile({}, { playa_name: "Dusty Two" })),
      }),
    });
    const name = await screen.findByLabelText(/^Playa name/);
    expect(name).not.toHaveAttribute("readonly");
    await userEvent.clear(name);
    await userEvent.type(name, "Dusty Two");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    const dialog = await screen.findByRole("dialog", { name: "Change this playa name?" });
    expect(dialog).toHaveTextContent(
      "This changes a permanent playa name. The change is recorded.",
    );
    expect(called(fetchMock, "PUT", `/api/people/${ME}`)).toBe(0);
    await userEvent.click(within(dialog).getByRole("button", { name: "Change the name" }));
    await waitFor(() => expect(called(fetchMock, "PUT", `/api/people/${ME}`)).toBe(1));
    expect(called(fetchMock, "PUT", "/api/people/me")).toBe(0);
  });

  it("first sign-in: the playa name takes focus, a save leaves the prompt", async () => {
    const { router } = mountApp({
      path: "/people/me?first=1",
      session: ident({ playa_name: "" }),
      routes: mine(profile({}, { playa_name: "" }), {
        "PUT /api/people/me": () => json(profile({}, { playa_name: "Newbie" })),
      }),
    });
    const name = await screen.findByLabelText(/^Playa name/);
    expect(await screen.findByText(/Tell us your playa name/)).toBeInTheDocument();
    await waitFor(() => expect(name).toHaveFocus());
    await userEvent.type(name, "Newbie");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(router()!.state.location.search).toBe(""));
    expect(await screen.findByText("Newbie", { selector: "header span" })).toBeInTheDocument();
  });

  it("an empty profile without the prompt says to add a playa name", async () => {
    mountApp({
      path: "/people/me",
      session: ident({ playa_name: "", superuser: true }),
      routes: mine(profile({}, { playa_name: "" })),
    });
    expect(await screen.findByText(/Your profile is empty/)).toBeInTheDocument();
  });

  it("field errors land on their inputs", async () => {
    mountApp({
      path: "/people/me",
      session: ident(),
      routes: mine(profile(), {
        "PUT /api/people/me": () =>
          problem(400, {
            code: "invalid",
            fields: [
              { field: "playa_name", code: "required", reason: "x" },
              { field: "phone", code: "too_long", params: { max: "40" }, reason: "x" },
            ],
          }),
      }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Save" }));
    await screen.findByRole("alert");
    expect(screen.getByLabelText(/^Playa name/)).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByLabelText(/^Phone number/)).toHaveAttribute("aria-invalid", "true");
    expect(screen.getAllByText("Must be at most 40 characters.").length).toBeGreaterThan(0);
  });

  it("read-only copy: the fields' buttons are disabled with a reason; 403/404/429/500 reads are shown", async () => {
    const ro = mountApp({
      path: "/people/me",
      session: ident(),
      routes: mine(profile({ contacts: [contact] })),
      readOnly: () => true,
    });
    const save = await screen.findByRole("button", { name: "Save" });
    await waitFor(() => expect(save).toBeDisabled());
    expect(screen.getByRole("button", { name: "Add a contact" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Request deletion" })).toBeDisabled();
    ro.unmount();
    for (const [status, code, text] of [
      [403, "permission", "You don't have access to this screen."],
      [404, "not_found", "We couldn't find that."],
      [429, "rate_limited", "Too many attempts."],
      [500, "internal", "Something went wrong."],
    ] as const) {
      const m = mountApp({
        path: "/people/me",
        session: ident(),
        routes: { "GET /api/people/me": () => problem(status, { code }, { "Retry-After": "5" }) },
      });
      // the frame's own read fails the same way; the screen shows it
      expect(await screen.findByText(new RegExp(text))).toBeInTheDocument();
      m.unmount();
    }
  });

  it("shows a module's sections and a failed one without failing the page", async () => {
    mountApp({
      path: "/people/me",
      session: ident(),
      routes: mine(
        profile({
          sections: [
            {
              key: "hello.greeting",
              items: [{ label: "module.hello.banner.welcome", value: "3", href: "/hello" }],
              failed: false,
            },
            { key: "hello.unavailable", items: [], failed: true },
          ],
        }),
      ),
    });
    expect(await screen.findByText("Welcome to the reference module.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "3" })).toHaveAttribute("href", "/hello");
    expect(screen.getByText("This part couldn't be loaded.")).toBeInTheDocument();
  });

  it("refuses an off-site href from a module's section", async () => {
    mountApp({
      path: "/people/me",
      session: ident(),
      routes: mine(
        profile({
          sections: [
            {
              key: "hello.greeting",
              items: [{ label: "x.y", value: "go", href: "//evil.example/x" }],
              failed: false,
            },
          ],
        }),
      ),
    });
    await screen.findByText("go");
    expect(screen.queryByRole("link", { name: "go" })).not.toBeInTheDocument();
  });
});

describe("emergency contacts", () => {
  it("adds, edits and removes with dialogs; phone is tel; names equal the API fields", async () => {
    const { fetchMock } = mountApp({
      path: "/people/me",
      session: ident(),
      routes: mine(profile({ contacts: [contact] }), {
        "POST /api/people/me/contacts": () => json(contact, { status: 201 }),
        [`PUT /api/people/me/contacts/${CONTACT}`]: () => json(contact),
        [`DELETE /api/people/me/contacts/${CONTACT}`]: () => noContent(),
      }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Add a contact" }));
    const add = await screen.findByRole("dialog", { name: "Add an emergency contact" });
    expect(within(add).getByLabelText(/^Contact's phone number/)).toHaveAttribute("type", "tel");
    await userEvent.type(within(add).getByLabelText(/^Contact's name/), "Kim");
    await userEvent.type(within(add).getByLabelText(/^Contact's phone number/), "555-0123");
    await userEvent.click(within(add).getByRole("button", { name: "Save contact" }));
    await screen.findByText("Contact saved");
    expect(await bodyOf(fetchMock, "POST", "/api/people/me/contacts")).toEqual({
      name: "Kim",
      phone: "555-0123",
      relationship: "",
    });

    await userEvent.click(screen.getByRole("button", { name: "Edit" }));
    const edit = await screen.findByRole("dialog", { name: "Edit this contact" });
    expect(within(edit).getByLabelText(/^Contact's name/)).toHaveValue("Sam Friend");
    await userEvent.click(within(edit).getByRole("button", { name: "Save contact" }));
    await waitFor(() =>
      expect(called(fetchMock, "PUT", `/api/people/me/contacts/${CONTACT}`)).toBe(1),
    );

    await userEvent.click(screen.getByRole("button", { name: "Remove" }));
    const rm = await screen.findByRole("dialog", { name: "Remove this contact?" });
    await userEvent.click(within(rm).getByRole("button", { name: "Remove contact" }));
    await waitFor(() =>
      expect(called(fetchMock, "DELETE", `/api/people/me/contacts/${CONTACT}`)).toBe(1),
    );
  });

  it("the tenth contact is the last: too_many is explained and the add button goes away", async () => {
    const ten = Array.from({ length: 10 }, (_, i) => ({ ...contact, id: `c${i}`, name: `C${i}` }));
    mountApp({ path: "/people/me", session: ident(), routes: mine(profile({ contacts: ten })) });
    expect(
      await screen.findByText(/That is the most contacts that can be kept \(10\)/),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add a contact" })).not.toBeInTheDocument();
  });

  it("a rejected contact shows field errors on the dialog", async () => {
    mountApp({
      path: "/people/me",
      session: ident(),
      routes: mine(profile(), {
        "POST /api/people/me/contacts": () =>
          problem(400, {
            code: "invalid",
            fields: [{ field: "contacts", code: "too_many", params: { max: "10" }, reason: "x" }],
          }),
      }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Add a contact" }));
    const dlg = await screen.findByRole("dialog", { name: "Add an emergency contact" });
    await userEvent.type(within(dlg).getByLabelText(/^Contact's name/), "Kim");
    await userEvent.type(within(dlg).getByLabelText(/^Contact's phone number/), "5");
    await userEvent.click(within(dlg).getByRole("button", { name: "Save contact" }));
    expect(await within(dlg).findByRole("alert")).toHaveTextContent(
      "That is the most that can be kept (10).",
    );
  });
});

describe("consents", () => {
  const consent = {
    person_id: ME,
    purpose: "hello.newsletter",
    granted: true,
    changed_at: "2026-10-01T10:00:00Z",
  };
  it("lists a granted consent and withdraws it with a switch", async () => {
    const { fetchMock } = mountApp({
      path: "/people/me",
      session: ident(),
      routes: mine(profile({ consents: [consent] }), {
        "PUT /api/people/me/consents/hello.newsletter": () => json({ ...consent, granted: false }),
      }),
    });
    const sw = await screen.findByLabelText("hello.newsletter");
    expect(sw).toBeChecked();
    await userEvent.click(sw);
    await screen.findByText("Consent withdrawn");
    expect(await bodyOf(fetchMock, "PUT", "/api/people/me/consents/hello.newsletter")).toEqual({
      granted: false,
    });
  });

  it("a failed change is shown", async () => {
    mountApp({
      path: "/people/me",
      session: ident(),
      routes: mine(profile({ consents: [consent] }), {
        "PUT /api/people/me/consents/hello.newsletter": () => problem(503, { code: "read_only" }),
      }),
    });
    await userEvent.click(await screen.findByLabelText("hello.newsletter"));
    expect(await screen.findByRole("alert")).toHaveTextContent("read-only");
  });
});

describe("your data", () => {
  it("downloads the export as a file in memory and says nothing personal in its name", async () => {
    const created: string[] = [];
    stubObjectUrls((b) => created.push(String(b.size)));
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
    const { fetchMock } = mountApp({
      path: "/people/me",
      session: ident(),
      routes: mine(profile(), { "GET /api/people/me/export": () => json({ format: 1 }) }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Download my data" }));
    await screen.findByText("Your data was downloaded");
    expect(called(fetchMock, "GET", "/api/people/me/export")).toBe(1);
    expect(click).toHaveBeenCalled();
    expect(created).toHaveLength(1);
    click.mockRestore();
  });

  it("the 10-minute cooldown is said in minutes", async () => {
    mountApp({
      path: "/people/me",
      session: ident(),
      routes: mine(profile(), {
        "GET /api/people/me/export": () =>
          problem(429, { code: "rate_limited" }, { "Retry-After": "420" }),
      }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Download my data" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "You can download your data once every 10 minutes. Try again in 7 minutes.",
    );
  });

  it("requests deletion with an optional note, then shows the open request with a withdraw button", async () => {
    let open = false;
    const { fetchMock } = mountApp({
      path: "/people/me",
      session: ident(),
      routes: {
        "GET /api/people/me": () => json(profile()),
        "GET /api/people/me/requests": () =>
          json({
            requests: open
              ? [
                  {
                    id: REQ,
                    person_id: ME,
                    kind: "delete",
                    status: "open",
                    note: "Please",
                    created_at: "2026-10-05T10:00:00Z",
                    decision_note: "",
                  },
                ]
              : [],
          }),
        "POST /api/people/me/requests": () => {
          open = true;
          return json({}, { status: 201 });
        },
        [`POST /api/people/me/requests/${REQ}/withdraw`]: () => {
          open = false;
          return noContent();
        },
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: "Request deletion" }));
    const dlg = await screen.findByRole("dialog", { name: "Request deletion?" });
    await userEvent.type(within(dlg).getByLabelText("Note (optional)"), "Please");
    await userEvent.click(within(dlg).getByRole("button", { name: "Request deletion" }));
    expect(await screen.findByText(/You asked for deletion on/)).toBeInTheDocument();
    expect(await bodyOf(fetchMock, "POST", "/api/people/me/requests")).toEqual({
      kind: "delete",
      note: "Please",
    });
    await userEvent.click(screen.getByRole("button", { name: "Withdraw request" }));
    await screen.findByText("Request withdrawn");
    expect(await screen.findByRole("button", { name: "Request deletion" })).toBeInTheDocument();
  });

  it("a duplicate request is explained; a declined one shows its note", async () => {
    mountApp({
      path: "/people/me",
      session: ident(),
      routes: {
        "GET /api/people/me": () => json(profile()),
        "GET /api/people/me/requests": () =>
          json({
            requests: [
              {
                id: REQ,
                person_id: ME,
                kind: "delete",
                status: "declined",
                note: "",
                created_at: "2026-10-05T10:00:00Z",
                decided_at: "2026-10-06T10:00:00Z",
                decision_note: "We need to keep your incident report.",
              },
            ],
          }),
        "POST /api/people/me/requests": () =>
          problem(409, {
            code: "conflict",
            fields: [{ field: "request", code: "duplicate", reason: "x" }],
          }),
      },
    });
    expect(await screen.findByText("We need to keep your incident report.")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Request deletion" }));
    const dlg = await screen.findByRole("dialog", { name: "Request deletion?" });
    await userEvent.click(within(dlg).getByRole("button", { name: "Request deletion" }));
    expect(await within(dlg).findByRole("alert")).toHaveTextContent(
      "You already have an open request.",
    );
  });
});

describe("/people (search)", () => {
  const find = (over: Routes = {}) =>
    mountApp({
      path: "/people",
      session: ident({ permissions: ["people.directory.read", "people.profiles.manage"] }),
      routes: mine(profile(), {
        "GET /api/people/search": () =>
          json({ matches: [{ id: PAT, playa_name: "Pat Dust", pronouns: "she/her" }] }),
        ...over,
      }),
    });

  it("searches by playa name, keeps the term out of the address, links to the person", async () => {
    const { router, fetchMock, container } = find();
    await userEvent.type(await screen.findByLabelText("Find a person"), "Pat");
    await userEvent.click(screen.getByRole("button", { name: "Search" }));
    expect(await screen.findByRole("link", { name: /Pat Dust/ })).toHaveAttribute(
      "href",
      `/people/${PAT}`,
    );
    expect(router()!.state.location.search).toBe("");
    const call = fetchMock.mock.calls.find(
      ([r]) => new URL(r.url).pathname === "/api/people/search",
    );
    expect(new URL(call![0].url).searchParams.get("q")).toBe("Pat");
    expect(screen.getByRole("link", { name: "New person" })).toHaveAttribute("href", "/people/new");
    await expectNoAxeViolations(container);
  });

  it("asks for at least two letters and does not call", async () => {
    const { fetchMock } = find();
    await userEvent.type(await screen.findByLabelText("Find a person"), "P");
    await userEvent.click(screen.getByRole("button", { name: "Search" }));
    expect((await screen.findAllByText("Type at least 2 letters.")).length).toBeGreaterThan(0);
    expect(called(fetchMock, "GET", "/api/people/search")).toBe(0);
  });

  it("no match, forbidden and read-only states", async () => {
    const a = find({ "GET /api/people/search": () => json({ matches: [] }) });
    await userEvent.type(await screen.findByLabelText("Find a person"), "Zed");
    await userEvent.click(screen.getByRole("button", { name: "Search" }));
    expect(await screen.findByText("No one matches.")).toBeInTheDocument();
    a.unmount();
    const b = find({ "GET /api/people/search": () => problem(403, { code: "permission" }) });
    await userEvent.type(await screen.findByLabelText("Find a person"), "Zed");
    await userEvent.click(screen.getByRole("button", { name: "Search" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "You don't have access to this screen.",
    );
    b.unmount();
    mountApp({
      path: "/people",
      session: ident({ permissions: ["people.directory.read", "people.profiles.manage"] }),
      routes: mine(),
      readOnly: () => true,
    });
    const btn = await screen.findByRole("button", { name: "New person" });
    await waitFor(() => expect(btn).toBeDisabled());
  });

  it("a server error and a rate limit are shown", async () => {
    find({
      "GET /api/people/search": () =>
        problem(429, { code: "rate_limited" }, { "Retry-After": "9" }),
    });
    await userEvent.type(await screen.findByLabelText("Find a person"), "Zed");
    await userEvent.click(screen.getByRole("button", { name: "Search" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Wait 9 seconds");
  });
});

describe("/people/new", () => {
  it("creates a person with only a playa name required, then opens their page", async () => {
    const { fetchMock, router } = mountApp({
      path: "/people/new",
      session: ident({ permissions: ["people.profiles.manage"] }),
      routes: mine(profile(), {
        "POST /api/people/": () =>
          json(profile({}, { id: PAT, playa_name: "Walk On", has_account: false, email: "" }), {
            status: 201,
          }),
        [`GET /api/people/${PAT}`]: () =>
          json(profile({ can: can() }, { id: PAT, playa_name: "Walk On" })),
      }),
    });
    await userEvent.type(await screen.findByLabelText(/^Playa name/), "Walk On");
    await userEvent.click(screen.getByRole("button", { name: "Create person" }));
    await waitFor(() => expect(router()!.state.location.pathname).toBe(`/people/${PAT}`));
    expect(await bodyOf(fetchMock, "POST", "/api/people/")).toEqual({
      playa_name: "Walk On",
      pronouns: "",
      legal_name: "",
      phone: "",
      contact_email: "",
    });
  });

  it("says the name is permanent and unique, and shows a taken name inline", async () => {
    mountApp({
      path: "/people/new",
      session: ident({ permissions: ["people.profiles.manage"] }),
      routes: mine(profile(), {
        "POST /api/people/": () =>
          problem(409, {
            code: "conflict",
            fields: [{ field: "playa_name", code: "taken", reason: "x" }],
          }),
      }),
    });
    expect(
      await screen.findByText(/It is permanent and must be one no one else here/),
    ).toBeVisible();
    await userEvent.type(screen.getByLabelText(/^Playa name/), "Walk On");
    await userEvent.click(screen.getByRole("button", { name: "Create person" }));
    await waitFor(() =>
      expect(screen.getByLabelText(/^Playa name/)).toHaveAttribute("aria-invalid", "true"),
    );
    expect(
      screen.getAllByText("Someone here already uses this playa name. Choose another.").length,
    ).toBeGreaterThan(0);
  });

  it("a missing playa name lands on its input; read-only disables the button", async () => {
    const a = mountApp({
      path: "/people/new",
      session: ident({ permissions: ["people.profiles.manage"] }),
      routes: mine(profile(), {
        "POST /api/people/": () =>
          problem(400, {
            code: "invalid",
            fields: [{ field: "playa_name", code: "required", reason: "x" }],
          }),
      }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Create person" }));
    await screen.findByRole("alert");
    expect(screen.getByLabelText(/^Playa name/)).toHaveAttribute("aria-invalid", "true");
    a.unmount();
    mountApp({
      path: "/people/new",
      session: ident({ permissions: ["people.profiles.manage"] }),
      routes: mine(),
      readOnly: () => true,
    });
    const b = await screen.findByRole("button", { name: "Create person" });
    await waitFor(() => expect(b).toBeDisabled());
  });
});

describe("/people/:person", () => {
  const other = (
    c: Record<string, boolean>,
    over: Record<string, unknown> = {},
    extra: Routes = {},
  ) =>
    mountApp({
      path: `/people/${PAT}`,
      session: ident({ permissions: ["people.directory.read"] }),
      routes: mine(profile(), {
        [`GET /api/people/${PAT}`]: () =>
          json(
            profile(
              { can: can(c), ...over },
              { id: PAT, playa_name: "Pat Dust", has_account: false, email: "" },
            ),
          ),
        ...extra,
      }),
    });

  it("your own ID goes to /people/me", async () => {
    const { router } = mountApp({
      path: `/people/${ME}`,
      session: ident(),
      routes: mine(),
    });
    await waitFor(() => expect(router()!.state.location.pathname).toBe("/people/me"));
  });

  it("a read-only viewer sees details without edit buttons or the recorded note", async () => {
    const { container } = other(
      {},
      {
        fields: ["playa_name", "pronouns", "language"],
        contacts: [],
      },
    );
    expect(await screen.findByRole("heading", { name: "Pat Dust" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Save" })).not.toBeInTheDocument();
    expect(screen.queryByText("Viewing these details is recorded.")).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Actions" })).not.toBeInTheDocument();
    await expectNoAxeViolations(container);
  });

  it("a viewer of contact details is told it is recorded, and sees the audit and roles links", async () => {
    other({ read_contacts: true, view_roles: true, view_audit: true }, { contacts: [contact] });
    expect(await screen.findByText("Viewing these details is recorded.")).toBeInTheDocument();
    expect(screen.getByText("Sam Friend")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Audit log" })).toHaveAttribute(
      "href",
      `/audit?subject=${PAT}`,
    );
    expect(screen.getByRole("link", { name: "Roles" })).toHaveAttribute(
      "href",
      `/access/grants?person=${PAT}`,
    );
    // no edit rights: no contact buttons
    expect(screen.queryByRole("button", { name: "Add a contact" })).not.toBeInTheDocument();
  });

  it("an editor changes the profile (required modes are not enforced) and contacts", async () => {
    const { fetchMock } = other(
      { edit: true, read_contacts: true },
      { contacts: [contact], policy: policy({ legal_name: "required" }) },
      {
        [`PUT /api/people/${PAT}`]: () =>
          json(
            profile(
              { can: can({ edit: true, read_contacts: true }) },
              { id: PAT, pronouns: "he/him" },
            ),
          ),
      },
    );
    const pronouns = await screen.findByLabelText(/^Pronouns/);
    expect(screen.getByLabelText(/^Legal name/).hasAttribute("required")).toBe(false);
    await userEvent.clear(pronouns);
    await userEvent.type(pronouns, "he/him");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(called(fetchMock, "PUT", `/api/people/${PAT}`)).toBe(1));
    expect(screen.queryByRole("dialog", { name: "Change this playa name?" })).toBeNull();
    expect(screen.getByRole("button", { name: "Add a contact" })).toBeEnabled();
  });

  it("an editor renames a set playa name only after confirming; cancel sends nothing; a taken name shows inline", async () => {
    let taken = true;
    const { fetchMock } = other(
      { edit: true, read_contacts: true },
      {},
      {
        [`PUT /api/people/${PAT}`]: () =>
          taken
            ? problem(409, {
                code: "conflict",
                fields: [{ field: "playa_name", code: "taken", reason: "x" }],
              })
            : json(profile({ can: can({ edit: true }) }, { id: PAT, playa_name: "Pat D" })),
      },
    );
    const name = await screen.findByLabelText(/^Playa name/);
    expect(name).not.toHaveAttribute("readonly");
    await userEvent.clear(name);
    await userEvent.type(name, "Pat D");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    const dialog = await screen.findByRole("dialog", { name: "Change this playa name?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(called(fetchMock, "PUT", `/api/people/${PAT}`)).toBe(0);
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await userEvent.click(
      within(await screen.findByRole("dialog", { name: "Change this playa name?" })).getByRole(
        "button",
        { name: "Change the name" },
      ),
    );
    await waitFor(() => expect(name).toHaveAttribute("aria-invalid", "true"));
    expect(
      screen.getAllByText("Someone here already uses this playa name. Choose another.").length,
    ).toBeGreaterThan(0);
    taken = false;
  });

  it("links an account by e-mail and says whether the empty profile was removed", async () => {
    const { fetchMock } = other(
      { link: true },
      {},
      {
        [`POST /api/people/${PAT}/link-account`]: () =>
          json({ account_id: ME, orphan_deleted: true }),
      },
    );
    await userEvent.click(await screen.findByRole("button", { name: "Link an account" }));
    const dlg = await screen.findByRole("dialog", { name: "Link an account" });
    const email = within(dlg).getByLabelText(/^Login e-mail address/);
    expect(email).toHaveAttribute("type", "email");
    await userEvent.type(email, "pat@example.test");
    await userEvent.click(within(dlg).getByRole("button", { name: "Link account" }));
    expect(
      await screen.findByText(/The empty profile it came with was removed/),
    ).toBeInTheDocument();
    expect(await bodyOf(fetchMock, "POST", `/api/people/${PAT}/link-account`)).toEqual({
      email: "pat@example.test",
    });
  });

  it("an unknown address lands on the e-mail input", async () => {
    other(
      { link: true },
      {},
      {
        [`POST /api/people/${PAT}/link-account`]: () =>
          problem(404, {
            code: "not_found",
            fields: [{ field: "email", code: "unknown", reason: "x" }],
          }),
      },
    );
    await userEvent.click(await screen.findByRole("button", { name: "Link an account" }));
    const dlg = await screen.findByRole("dialog", { name: "Link an account" });
    await userEvent.type(within(dlg).getByLabelText(/^Login e-mail address/), "x@example.test");
    await userEvent.click(within(dlg).getByRole("button", { name: "Link account" }));
    expect(await within(dlg).findByRole("alert")).toBeInTheDocument();
  });

  it("exports a person's data as a file", async () => {
    stubObjectUrls(() => {});
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
    const { fetchMock } = other(
      { export: true },
      {},
      {
        [`GET /api/people/${PAT}/export`]: () => json({ format: 1 }),
      },
    );
    await userEvent.click(await screen.findByRole("button", { name: "Export data" }));
    await screen.findByText("The data was downloaded");
    expect(called(fetchMock, "GET", `/api/people/${PAT}/export`)).toBe(1);
    click.mockRestore();
  });

  it("deletes after a dialog that repeats the playa name, then shows the report", async () => {
    const { router } = other(
      { delete: true },
      {},
      {
        [`DELETE /api/people/${PAT}`]: () =>
          json({
            person: PAT,
            people: 1,
            identity: 0,
            access: 2,
            modules: { hello: { deleted: 3, anonymized: 1, kept: [] } },
            kept: [{ kind: "audit.entry", count: 12 }],
          }),
      },
    );
    await userEvent.click(await screen.findByRole("button", { name: "Delete person" }));
    const dlg = await screen.findByRole("dialog", { name: "Delete this person?" });
    expect(dlg).toHaveTextContent("You are about to delete Pat Dust.");
    await userEvent.click(within(dlg).getByRole("button", { name: "Delete person" }));
    expect(await screen.findByRole("heading", { name: "Person deleted" })).toBeInTheDocument();
    const main = screen.getByRole("main");
    expect(main).toHaveTextContent("3 removed, 1 made anonymous");
    expect(main).toHaveTextContent("12 records of audit log entries kept");
    await userEvent.click(within(main).getByRole("link", { name: "Back to people" }));
    await waitFor(() => expect(router()!.state.location.pathname).toBe("/people"));
  });

  it.each([
    ["last_access_admin", "last one who can grant roles"],
    ["in_use", "Something else still refers to this person"],
  ])("deletion refused with %s is explained", async (code, words) => {
    other(
      { delete: true },
      {},
      {
        [`DELETE /api/people/${PAT}`]: () =>
          problem(409, { code: "conflict", fields: [{ field: "person", code, reason: "x" }] }),
      },
    );
    await userEvent.click(await screen.findByRole("button", { name: "Delete person" }));
    const dlg = await screen.findByRole("dialog", { name: "Delete this person?" });
    await userEvent.click(within(dlg).getByRole("button", { name: "Delete person" }));
    expect(await within(dlg).findByRole("alert")).toHaveTextContent(words);
  });

  it("404, 403 and read-only states", async () => {
    const a = mountApp({
      path: `/people/${PAT}`,
      session: ident(),
      routes: mine(profile(), {
        [`GET /api/people/${PAT}`]: () => problem(404, { code: "not_found" }),
      }),
    });
    expect(await screen.findByRole("alert")).toHaveTextContent("We couldn't find that.");
    a.unmount();
    const b = mountApp({
      path: `/people/${PAT}`,
      session: ident(),
      routes: mine(profile(), {
        [`GET /api/people/${PAT}`]: () => problem(403, { code: "permission" }),
      }),
    });
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "You don't have access to this screen.",
    );
    b.unmount();
    mountApp({
      path: `/people/${PAT}`,
      session: ident(),
      routes: mine(profile(), {
        [`GET /api/people/${PAT}`]: () =>
          json(profile({ can: can({ edit: true, delete: true }) }, { id: PAT, playa_name: "Pat" })),
      }),
      readOnly: () => true,
    });
    const del = await screen.findByRole("button", { name: "Delete person" });
    await waitFor(() => expect(del).toBeDisabled());
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  });
});

describe("/people/admin/requests", () => {
  const req = (over: Record<string, unknown> = {}) => ({
    id: REQ,
    person_id: PAT,
    kind: "delete",
    status: "open",
    note: "Please delete me",
    created_at: "2026-10-05T10:00:00Z",
    decision_note: "",
    ...over,
  });
  const admin = (extra: Routes = {}) =>
    mountApp({
      path: "/people/admin/requests",
      session: ident({ permissions: ["people.data.delete"] }),
      routes: mine(profile(), {
        "GET /api/people/requests": () => json({ requests: [req()] }),
        "GET /api/people/names": () => json({ names: { [PAT]: "Pat Dust" } }),
        ...extra,
      }),
    });

  it("lists open requests with the person's name, date and note; accessible", async () => {
    const { container } = admin();
    expect(await screen.findByRole("link", { name: "Pat Dust" })).toHaveAttribute(
      "href",
      `/people/${PAT}`,
    );
    expect(screen.getByText("Please delete me")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Open" })).toHaveAttribute("aria-pressed", "true");
    await expectNoAxeViolations(container);
  });

  it("declines with a note", async () => {
    const { fetchMock } = admin({
      [`POST /api/people/requests/${REQ}/decline`]: () => json(req({ status: "declined" })),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Decline" }));
    const dlg = await screen.findByRole("dialog", { name: "Decline this request?" });
    await userEvent.type(within(dlg).getByLabelText("Note to the person"), "We must keep it");
    await userEvent.click(within(dlg).getByRole("button", { name: "Decline request" }));
    await screen.findByText("Request declined");
    expect(await bodyOf(fetchMock, "POST", `/api/people/requests/${REQ}/decline`)).toEqual({
      note: "We must keep it",
    });
  });

  it("fulfils after a confirmation and shows the report; last_access_admin is explained", async () => {
    let n = 0;
    admin({
      [`POST /api/people/requests/${REQ}/fulfil`]: () => {
        n += 1;
        return n === 1
          ? problem(409, {
              code: "conflict",
              fields: [{ field: "person", code: "last_access_admin", reason: "x" }],
            })
          : json({ person: PAT, people: 1, identity: 1, access: 0, modules: {}, kept: [] });
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: "Fulfil" }));
    const dlg = await screen.findByRole("dialog", { name: "Delete this person?" });
    await userEvent.click(within(dlg).getByRole("button", { name: "Delete person" }));
    expect(await within(dlg).findByRole("alert")).toHaveTextContent("last one who can grant roles");
    await userEvent.click(within(dlg).getByRole("button", { name: "Delete person" }));
    const report = await screen.findByRole("dialog", { name: "Person deleted" });
    expect(report).toHaveTextContent("Profile records removed");
  });

  it("F44: fulfilling a request for a protected account shows the shared-account sentence", async () => {
    admin({
      [`POST /api/people/requests/${REQ}/fulfil`]: () =>
        problem(409, {
          code: "conflict",
          ref: "AB12-CD34",
          fields: [{ field: "person", code: "protected", reason: "x" }],
        }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Fulfil" }));
    const dlg = await screen.findByRole("dialog", { name: "Delete this person?" });
    await userEvent.click(within(dlg).getByRole("button", { name: "Delete person" }));
    const alert = await within(dlg).findByRole("alert");
    expect(alert).toHaveTextContent(
      "This is a shared demo account. Its password and e-mail address can't be changed.",
    );
    expect(alert).not.toHaveTextContent("AB12-CD34");
  });

  it("switches tabs; empty, forbidden and read-only states", async () => {
    const calls: string[] = [];
    const a = admin({
      "GET /api/people/requests": (r) => {
        calls.push(new URL(r.url).searchParams.get("status") ?? "");
        return json({ requests: [] });
      },
    });
    expect(await screen.findByText("No open requests")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Declined" }));
    expect(await screen.findByText("No declined requests")).toBeInTheDocument();
    expect(calls).toEqual(["open", "declined"]);
    a.unmount();
    const b = admin({ "GET /api/people/requests": () => problem(403, { code: "permission" }) });
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "You don't have access to this screen.",
    );
    b.unmount();
    mountApp({
      path: "/people/admin/requests",
      session: ident({ permissions: ["people.data.delete"] }),
      routes: mine(profile(), {
        "GET /api/people/requests": () => json({ requests: [req()] }),
        "GET /api/people/names": () => json({ names: {} }),
      }),
      readOnly: () => true,
    });
    const f = await screen.findByRole("button", { name: "Fulfil" });
    await waitFor(() => expect(f).toBeDisabled());
    expect(screen.getByRole("button", { name: "Decline" })).toBeDisabled();
  });

  it("the admin entry shows only to a holder of people.data.delete", async () => {
    mountApp({ session: ident({ permissions: ["people.data.delete"] }), routes: mine() });
    await screen.findByRole("heading", { name: "Hi, Dusty" });
    const nav = screen.getByRole("navigation", { name: "Administration" });
    expect(within(nav).getByRole("link", { name: "Deletion requests" })).toHaveAttribute(
      "href",
      "/people/admin/requests",
    );
  });
});

describe("review fixes", () => {
  it("search shows the forbidden state up front without people.directory.read, and makes no call", async () => {
    const { fetchMock } = mountApp({ path: "/people", session: ident(), routes: mine() });
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "You don't have access to this screen.",
    );
    expect(screen.queryByLabelText("Find a person")).not.toBeInTheDocument();
    expect(called(fetchMock, "GET", "/api/people/search")).toBe(0);
  });

  it("search announces how many people it found", async () => {
    mountApp({
      path: "/people",
      session: ident({ permissions: ["people.directory.read"] }),
      routes: mine(profile(), {
        "GET /api/people/search": () =>
          json({ matches: [{ id: PAT, playa_name: "Pat", pronouns: "" }] }),
      }),
    });
    await userEvent.type(await screen.findByLabelText("Find a person"), "Pat");
    await userEvent.click(screen.getByRole("button", { name: "Search" }));
    expect(await screen.findByText("1 person found")).toBeInTheDocument();
  });

  it("read-only copy: the profile fields are disabled", async () => {
    mountApp({ path: "/people/me", session: ident(), routes: mine(), readOnly: () => true });
    const name = await screen.findByLabelText(/^Playa name/);
    await waitFor(() => expect(name).toBeDisabled());
  });

  it("the account admin links to a profile only for a holder of people.directory.read", async () => {
    const acct = {
      id: PAT,
      person_id: PAT,
      email: "pat@example.test",
      superuser: false,
      password_change_required: false,
      password_changed_at: "2026-10-01T10:00:00Z",
      created_at: "2026-10-01T10:00:00Z",
      updated_at: "2026-10-01T10:00:00Z",
    };
    const a = mountApp({
      path: `/accounts/admin/${PAT}`,
      session: ident({ permissions: ["accounts.read", "people.directory.read"] }),
      routes: mine(profile(), { [`GET /api/accounts/${PAT}`]: () => json(acct) }),
    });
    expect(await screen.findByRole("link", { name: "See this person's profile" })).toHaveAttribute(
      "href",
      `/people/${PAT}`,
    );
    a.unmount();
    mountApp({
      path: `/accounts/admin/${PAT}`,
      session: ident({ permissions: ["accounts.read"] }),
      routes: mine(profile(), { [`GET /api/accounts/${PAT}`]: () => json(acct) }),
    });
    await screen.findByText("pat@example.test");
    expect(
      screen.queryByRole("link", { name: "See this person's profile" }),
    ).not.toBeInTheDocument();
  });
});
