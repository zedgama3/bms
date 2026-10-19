import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { expectNoAxeViolations } from "@/test/axe";
import { mountApp } from "@/test/app";
import { json, problem } from "@/test/fakeFetch";

const ME = "7d2c2f0e-0000-4000-8000-000000000001";
const LEAD = "7d2c2f0e-0000-4000-8000-000000000009";
const D1 = "5c1a0000-0000-4000-8000-0000000000d1";
const D2 = "5c1a0000-0000-4000-8000-0000000000d2";

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
const dept = (over: Record<string, unknown> = {}) => ({
  id: D1,
  name: "Rangers",
  description: "Look after people.\nSecond line that stays off the card.",
  contact_email: "rangers@example.test",
  archived: false,
  created_at: "2026-10-01T10:00:00Z",
  updated_at: "2026-10-02T10:00:00Z",
  ...over,
});
const archivedDept = dept({
  id: D2,
  name: "Old Gate",
  archived: true,
  archived_at: "2026-09-01T10:00:00Z",
});

type Routes = Record<string, (req: Request) => Response | Promise<Response>>;
const base = (over: Routes = {}): Routes => ({
  "GET /api/departments/": () => json({ departments: [dept()] }),
  [`GET /api/departments/${D1}`]: () => json(dept()),
  "GET /api/people/me": () => problem(404, { code: "not_found" }),
  "GET /api/people/names": () => json({ names: { [LEAD]: "Lee Lead" } }),
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
const seen = (fetchMock: { mock: { calls: [Request][] } }, method: string, path: string) =>
  fetchMock.mock.calls.some(([r]) => r.method === method && new URL(r.url).pathname === path);

describe("navigation", () => {
  it("lists Departments in the main navigation for any signed-in person", async () => {
    mountApp({ session: ident([]), routes: base() });
    await screen.findByRole("heading", { name: "Hi, Dusty" });
    const main = screen.getByRole("navigation", { name: "Main" });
    expect(within(main).getByRole("link", { name: "Departments" })).toHaveAttribute(
      "href",
      "/departments",
    );
  });
});

describe("/departments", () => {
  it("shows cards with the first line of the description and the mailbox as a mailto link", async () => {
    const { container } = mountApp({ path: "/departments", session: ident([]), routes: base() });
    expect(await screen.findByRole("link", { name: "Rangers" })).toHaveAttribute(
      "href",
      `/departments/${D1}`,
    );
    expect(screen.getByText("Look after people.")).toBeInTheDocument();
    expect(screen.queryByText(/Second line/)).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "rangers@example.test" })).toHaveAttribute(
      "href",
      "mailto:rangers@example.test",
    );
    // a participant gets neither the button nor the toggle
    expect(screen.queryByRole("link", { name: "New department" })).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Show archived")).not.toBeInTheDocument();
    await expectNoAxeViolations(container);
  });

  it("a steward gets New and the archived toggle, which asks for archived ones", async () => {
    const { fetchMock, router } = mountApp({
      path: "/departments",
      session: ident(["departments.manage"]),
      routes: base({
        "GET /api/departments/": (req) =>
          json({
            departments: new URL(req.url).searchParams.get("include_archived")
              ? [dept(), archivedDept]
              : [dept()],
          }),
      }),
    });
    expect(await screen.findByRole("link", { name: "New department" })).toHaveAttribute(
      "href",
      "/departments/new",
    );
    await userEvent.click(screen.getByLabelText("Show archived"));
    expect(await screen.findByRole("link", { name: "Old Gate" })).toBeInTheDocument();
    expect(screen.getByText("Archived")).toBeInTheDocument();
    expect(router()!.state.location.search).toBe("?archived=1");
    expect(
      fetchMock.mock.calls.some(([r]) => new URL(r.url).search === "?include_archived=true"),
    ).toBe(true);
  });

  it("says there are none, adding the steward sentence only for stewards", async () => {
    const routes = base({ "GET /api/departments/": () => json({ departments: [] }) });
    const first = mountApp({ path: "/departments", session: ident([]), routes });
    expect(await screen.findByText("No department has been created yet.")).toBeInTheDocument();
    first.unmount();
    mountApp({ path: "/departments", session: ident(["departments.manage"]), routes });
    expect(
      await screen.findByText("No department has been created yet. Create the first one."),
    ).toBeInTheDocument();
  });

  it("shows a failed load with a retry, and a disabled New on a read-only copy", async () => {
    let n = 0;
    const view = mountApp({
      path: "/departments",
      session: ident(["departments.manage"]),
      readOnly: () => true,
      routes: base({
        "GET /api/departments/": () => {
          n += 1;
          return n === 1
            ? problem(500, { code: "internal", ref: "AAAA-0000" })
            : json({ departments: [dept()] });
        },
      }),
    });
    expect(await screen.findByRole("alert")).toHaveTextContent("Something went wrong.");
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    await screen.findByRole("link", { name: "Rangers" });
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "New department" })).toBeDisabled(),
    );
    expect(screen.getByRole("button", { name: "New department" })).toHaveAccessibleDescription(
      /read-only/,
    );
    view.unmount();
  });
});

describe("/departments/new", () => {
  it("is the forbidden screen without departments.manage", async () => {
    mountApp({ path: "/departments/new", session: ident([]), routes: base() });
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "You don't have access to this screen.",
    );
  });

  it("explains the mailbox, creates the department and opens its page", async () => {
    const { fetchMock, router } = mountApp({
      path: "/departments/new",
      session: ident(["departments.manage"]),
      routes: base({
        "POST /api/departments/": () => json(dept(), { status: 201 }),
      }),
    });
    const email = await screen.findByLabelText("Contact mailbox");
    expect(email).toHaveAttribute("type", "email");
    expect(email).toHaveAttribute("inputmode", "email");
    expect(email).toHaveAccessibleDescription(
      "The department's shared mailbox, never a person's address.",
    );
    await userEvent.type(screen.getByLabelText(/^Name/), "Rangers");
    await userEvent.type(screen.getByLabelText("Description"), "Look after people.");
    await userEvent.type(email, "rangers@example.test");
    await userEvent.click(screen.getByRole("button", { name: "Create department" }));
    await waitFor(() => expect(router()!.state.location.pathname).toBe(`/departments/${D1}`));
    expect(await bodyOf(fetchMock, "POST", "/api/departments/")).toEqual({
      name: "Rangers",
      description: "Look after people.",
      contact_email: "rangers@example.test",
    });
  });

  it("puts field errors on their inputs (taken, malformed)", async () => {
    mountApp({
      path: "/departments/new",
      session: ident(["departments.manage"]),
      routes: base({
        "POST /api/departments/": () =>
          problem(409, {
            code: "conflict",
            fields: [
              { field: "name", code: "taken" },
              { field: "contact_email", code: "malformed" },
            ],
          }),
      }),
    });
    await userEvent.type(await screen.findByLabelText(/^Name/), "Rangers");
    await userEvent.click(screen.getByRole("button", { name: "Create department" }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Another department already has this name.");
    expect(screen.getByLabelText(/^Name/)).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByLabelText("Contact mailbox")).toHaveAttribute("aria-invalid", "true");
  });
});

describe("/departments/:department", () => {
  const leadGrant = {
    id: "0b6f1c3e-0000-4000-8000-0000000000b1",
    person_id: LEAD,
    role_id: "0b6f1c3e-0000-4000-8000-0000000000a3",
    role_key: "department-admin",
    role_name: "",
    department_id: D1,
    created_at: "2026-10-02T10:00:00Z",
    ended: false,
  };

  it("shows a participant the details and the mailbox, with no actions and no grants call", async () => {
    const { fetchMock, container } = mountApp({
      path: `/departments/${D1}`,
      session: ident([]),
      routes: base(),
    });
    expect(await screen.findByRole("heading", { level: 1, name: "Rangers" })).toBeInTheDocument();
    expect(screen.getByText(/Second line that stays off the card/)).toBeInTheDocument();
    for (const name of ["Edit details", "Rename", "Archive", "Restore"])
      expect(screen.queryByRole("button", { name })).not.toBeInTheDocument();
    const card = screen.getByRole("region", { name: "Leadership" });
    expect(within(card).getByRole("link", { name: "rangers@example.test" })).toBeInTheDocument();
    expect(within(card).queryByRole("link", { name: "See roles here" })).not.toBeInTheDocument();
    expect(seen(fetchMock, "GET", "/api/access/grants")).toBe(false);
    await expectNoAxeViolations(container);
  });

  it("shows holders of access.grants.read the leads and the grant links", async () => {
    const { fetchMock } = mountApp({
      path: `/departments/${D1}`,
      session: ident(["access.grants.read", "access.department-grants.manage"]),
      routes: base({
        "GET /api/access/grants": () =>
          json({
            grants: [
              leadGrant,
              { ...leadGrant, id: "x2", person_id: ME, role_key: "department-steward" },
            ],
          }),
      }),
    });
    const card = await screen.findByRole("region", { name: "Leadership" });
    expect(await within(card).findByText("Lee Lead")).toBeInTheDocument();
    expect(within(card).getAllByRole("listitem")).toHaveLength(1);
    expect(within(card).getByRole("link", { name: "See roles here" })).toHaveAttribute(
      "href",
      `/access/grants?department=${D1}&role=department-admin`,
    );
    expect(within(card).getByRole("link", { name: "Appoint a lead" })).toHaveAttribute(
      "href",
      `/access/grants/new?department=${D1}`,
    );
    const call = fetchMock.mock.calls.find(
      ([r]) => new URL(r.url).pathname === "/api/access/grants",
    );
    expect(new URL(call![0].url).search).toBe(`?department=${D1}&role=department-admin`);
  });

  it("a department admin at it can edit details but not rename or archive", async () => {
    const { fetchMock } = mountApp({
      path: `/departments/${D1}`,
      session: ident([`departments.details.manage@${D1}`]),
      routes: base({
        [`PUT /api/departments/${D1}`]: () => json(dept({ description: "New words." })),
      }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Edit details" }));
    expect(screen.queryByRole("button", { name: "Rename" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Archive" })).not.toBeInTheDocument();
    const dialog = await screen.findByRole("dialog", { name: "Edit details" });
    const description = within(dialog).getByLabelText("Description");
    await userEvent.clear(description);
    await userEvent.type(description, "New words.");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save details" }));
    await screen.findByText("Saved");
    expect(await bodyOf(fetchMock, "PUT", `/api/departments/${D1}`)).toEqual({
      description: "New words.",
      contact_email: "rangers@example.test",
    });
  });

  it("a steward renames; a taken name lands on the input", async () => {
    let attempt = 0;
    const { fetchMock } = mountApp({
      path: `/departments/${D1}`,
      session: ident(["departments.manage"]),
      routes: base({
        [`PUT /api/departments/${D1}`]: () => {
          attempt += 1;
          return attempt === 1
            ? problem(409, { code: "conflict", fields: [{ field: "name", code: "taken" }] })
            : json(dept({ name: "Park Rangers" }));
        },
      }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Rename" }));
    const dialog = await screen.findByRole("dialog", { name: "Rename" });
    const name = within(dialog).getByLabelText(/^Name/);
    await userEvent.clear(name);
    await userEvent.type(name, "Park Rangers");
    await userEvent.click(within(dialog).getByRole("button", { name: "Rename" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("Another department");
    expect(name).toHaveAttribute("aria-invalid", "true");
    await userEvent.click(within(dialog).getByRole("button", { name: "Rename" }));
    await screen.findByText("Renamed");
    expect(await bodyOf(fetchMock, "PUT", `/api/departments/${D1}`)).toEqual({
      name: "Park Rangers",
    });
  });

  it("archives after a confirmation that says leads keep their roles", async () => {
    const { fetchMock } = mountApp({
      path: `/departments/${D1}`,
      session: ident(["departments.manage"]),
      routes: base({
        [`POST /api/departments/${D1}/archive`]: () =>
          json(dept({ archived: true, archived_at: "2026-10-10T10:00:00Z" })),
      }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Archive" }));
    const dialog = await screen.findByRole("dialog", { name: "Archive this department?" });
    expect(dialog).toHaveTextContent("Its leads keep their roles");
    expect(dialog).toHaveTextContent("you can restore it");
    await userEvent.click(within(dialog).getByRole("button", { name: "Archive department" }));
    await screen.findByText("Archived");
    expect(seen(fetchMock, "POST", `/api/departments/${D1}/archive`)).toBe(true);
  });

  it("an archived department shows a banner and only Restore; restoring posts", async () => {
    const { fetchMock } = mountApp({
      path: `/departments/${D2}`,
      session: ident(["departments.manage", `departments.details.manage@${D2}`]),
      routes: base({
        [`GET /api/departments/${D2}`]: () => json(archivedDept),
        [`POST /api/departments/${D2}/restore`]: () => json(dept({ id: D2 })),
      }),
    });
    expect(await screen.findByText(/^Archived on /)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Edit details" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Rename" })).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Appoint a lead" })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Restore" }));
    const dialog = await screen.findByRole("dialog", { name: "Restore this department?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Restore department" }));
    await screen.findByText("Restored");
    expect(seen(fetchMock, "POST", `/api/departments/${D2}/restore`)).toBe(true);
  });

  it("shows the not-found state for an unknown department and a refusal from the server", async () => {
    const first = mountApp({
      path: `/departments/${D2}`,
      session: ident([]),
      routes: base({ [`GET /api/departments/${D2}`]: () => problem(404, { code: "not_found" }) }),
    });
    expect(await screen.findByRole("alert")).toHaveTextContent("We couldn't find that.");
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
    first.unmount();
    mountApp({
      path: `/departments/${D1}`,
      session: ident(["departments.manage"]),
      routes: base({
        [`POST /api/departments/${D1}/archive`]: () =>
          problem(409, { code: "conflict", fields: [{ field: "body", code: "archived" }] }),
      }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Archive" }));
    const dialog = await screen.findByRole("dialog", { name: "Archive this department?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Archive department" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("is archived");
  });

  it("a failed leads call is shown inside the card", async () => {
    mountApp({
      path: `/departments/${D1}`,
      session: ident(["access.grants.read"]),
      routes: base({ "GET /api/access/grants": () => problem(403, { code: "permission" }) }),
    });
    const card = await screen.findByRole("region", { name: "Leadership" });
    expect(await within(card).findByRole("alert")).toHaveTextContent(
      "You don't have access to this screen.",
    );
  });

  it("reads every page of grants and never shows raw IDs when names fail", async () => {
    const calls: string[] = [];
    mountApp({
      path: `/departments/${D1}`,
      session: ident(["access.grants.read"]),
      routes: base({
        "GET /api/access/grants": (req) => {
          const after = new URL(req.url).searchParams.get("after") ?? "";
          calls.push(after);
          return after
            ? json({ grants: [leadGrant] })
            : json({
                grants: [{ ...leadGrant, id: "x2", role_key: "department-steward" }],
                next: "c2",
              });
        },
        "GET /api/people/names": () => problem(500, { code: "internal", ref: "AAAA-0000" }),
      }),
    });
    const card = await screen.findByRole("region", { name: "Leadership" });
    expect(await within(card).findByText("Unknown person")).toBeInTheDocument();
    expect(card).not.toHaveTextContent(LEAD);
    expect(await within(card).findByRole("alert")).toHaveTextContent("Something went wrong.");
    expect(within(card).getByRole("button", { name: "Try again" })).toBeInTheDocument();
    expect(calls).toEqual(["", "c2"]);
  });
});

describe("leadership cap", () => {
  it("says so when the page cap is hit instead of cutting the list silently", async () => {
    mountApp({
      path: `/departments/${D1}`,
      session: ident(["access.grants.read"]),
      routes: base({
        "GET /api/access/grants": () =>
          json({
            grants: [
              {
                id: "g1",
                person_id: LEAD,
                role_id: "r1",
                role_key: "department-steward",
                role_name: "",
                department_id: D1,
                created_at: "2026-10-02T10:00:00Z",
                ended: false,
              },
            ],
            next: "more",
          }),
      }),
    });
    const card = await screen.findByRole("region", { name: "Leadership" });
    expect(
      await within(card).findByText(/This list is cut short/, {}, { timeout: 4000 }),
    ).toBeInTheDocument();
  }, 10000);
});

describe("catalogue", () => {
  it("names the module and its permissions for the role editor and the module list", async () => {
    const en = (await import("./messages/en.json")).default as Record<string, string>;
    for (const k of [
      "module.departments.title",
      "module.departments.permission.manage.title",
      "module.departments.permission.details.manage.title",
    ])
      expect(en[k]).toBeTruthy();
    const ev = (await import("../events/messages/en.json")).default as Record<string, string>;
    expect(ev["module.events.title"]).toBeTruthy();
    expect(ev["module.events.permission.manage.title"]).toBeTruthy();
  });
});
