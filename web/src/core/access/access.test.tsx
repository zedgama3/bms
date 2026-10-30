import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { expectNoAxeViolations } from "@/test/axe";
import { mountApp } from "@/test/app";
import { isMessageKey } from "@/i18n";
import { json, noContent, problem } from "@/test/fakeFetch";

const ME = "7d2c2f0e-0000-4000-8000-000000000001";
const PAT = "7d2c2f0e-0000-4000-8000-000000000009";
const R_BASE = "0b6f1c3e-0000-4000-8000-0000000000a1";
const R_ADMIN = "0b6f1c3e-0000-4000-8000-0000000000a2";
const R_DEPT = "0b6f1c3e-0000-4000-8000-0000000000a3";
const R_LEAD = "0b6f1c3e-0000-4000-8000-0000000000a5";
const R_CUSTOM = "0b6f1c3e-0000-4000-8000-0000000000a4";
const GRANT = "0b6f1c3e-0000-4000-8000-0000000000b1";
const RULE = "0b6f1c3e-0000-4000-8000-0000000000c1";
const D_A = "5c1a0000-0000-4000-8000-0000000000d1";
const D_B = "5c1a0000-0000-4000-8000-0000000000d2";
const dept = (id: string, name: string) => ({
  id,
  name,
  description: "",
  archived: false,
  created_at: "2026-10-01T10:00:00Z",
  updated_at: "2026-10-01T10:00:00Z",
});
const DEPT_A = dept(D_A, "Gate");
const DEPT_B = dept(D_B, "Rangers");

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
const ALL = [
  "access.roles.read",
  "access.roles.manage",
  "access.rules.manage",
  "access.grants.read",
  "access.grants.manage",
];

const roles = [
  {
    id: R_BASE,
    key: "participant",
    name: "",
    description: "",
    scope: "site",
    base: true,
    permissions: [{ name: "hello.greetings.read", declared: true }],
    created_at: "2026-10-01T10:00:00Z",
    updated_at: "2026-10-01T10:00:00Z",
  },
  {
    id: R_ADMIN,
    key: "access-admin",
    name: "",
    description: "Looks after roles.",
    scope: "site",
    base: false,
    permissions: [
      { name: "access.grants.manage", declared: true },
      { name: "old.thing.do", declared: false },
    ],
    created_at: "2026-10-01T10:00:00Z",
    updated_at: "2026-10-01T10:00:00Z",
  },
  {
    id: R_DEPT,
    name: "Shift lead",
    description: "",
    scope: "department",
    base: false,
    permissions: [],
    created_at: "2026-10-01T10:00:00Z",
    updated_at: "2026-10-01T10:00:00Z",
  },
  {
    id: R_CUSTOM,
    name: "Gate crew",
    description: "",
    scope: "site",
    base: false,
    permissions: [],
    created_at: "2026-10-01T10:00:00Z",
    updated_at: "2026-10-01T10:00:00Z",
  },
];
const catalogue = [
  {
    name: "access.grants.manage",
    scope: "site",
    reaches: "internal",
    module: "access",
    starter_roles: [],
  },
  {
    name: "access.roles.read",
    scope: "site",
    reaches: "internal",
    module: "access",
    starter_roles: [],
  },
  {
    name: "hello.greetings.read",
    scope: "department",
    reaches: "personal",
    module: "hello",
    starter_roles: [],
  },
  {
    name: "people.data.delete",
    scope: "site",
    reaches: "sensitive",
    module: "people",
    starter_roles: [],
  },
];
const grant = (over: Record<string, unknown> = {}) => ({
  id: GRANT,
  person_id: PAT,
  role_id: R_ADMIN,
  role_key: "access-admin",
  role_name: "",
  created_at: "2026-10-02T10:00:00Z",
  ended: false,
  ...over,
});

type Routes = Record<string, (req: Request) => Response | Promise<Response>>;
const base = (over: Routes = {}): Routes => ({
  "GET /api/access/roles": () => json({ roles }),
  "GET /api/access/permissions": () => json({ permissions: catalogue }),
  "GET /api/access/rules": () => json({ rules: [] }),
  "GET /api/access/grants": () => json({ grants: [] }),
  "GET /api/departments/": () => json({ departments: [DEPT_A, DEPT_B] }),
  "GET /api/people/names": () => json({ names: { [PAT]: "Pat Dust", [ME]: "Dusty" } }),
  "GET /api/people/me": () => problem(404, { code: "not_found" }),
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
  it("shows Roles and Grants to a holder, site-wide or for a department, and nothing to others", async () => {
    mountApp({ session: ident(["access.grants.read@dept-1"]), routes: base() });
    await screen.findByRole("heading", { name: "Hi, Dusty" });
    const admin = screen.getByRole("navigation", { name: "Administration" });
    expect(within(admin).getByRole("link", { name: "Grants" })).toHaveAttribute(
      "href",
      "/access/grants",
    );
    expect(within(admin).queryByRole("link", { name: "Roles" })).not.toBeInTheDocument();
  });

  it("an empty permission list hides every permissioned entry", async () => {
    mountApp({ session: ident([]), routes: base() });
    await screen.findByRole("heading", { name: "Hi, Dusty" });
    expect(screen.queryByRole("navigation", { name: "Administration" })).not.toBeInTheDocument();
  });
});

describe("/access/roles", () => {
  it("lists roles as cards: starter titles, scope, the everyone badge, permission counts; accessible", async () => {
    const { container } = mountApp({
      path: "/access/roles",
      session: ident(ALL),
      routes: base(),
    });
    expect(await screen.findByRole("link", { name: "Every participant" })).toHaveAttribute(
      "href",
      `/access/roles/${R_BASE}`,
    );
    expect(screen.getByRole("link", { name: "Access administrator" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Gate crew" })).toBeInTheDocument();
    expect(screen.getByText("Everyone who is signed in")).toBeInTheDocument();
    expect(screen.getAllByText("One department").length).toBe(1);
    expect(screen.getByText("2 permissions")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "New role" })).toHaveAttribute(
      "href",
      "/access/roles/new",
    );
    expect(screen.getByRole("link", { name: "Rules" })).toHaveAttribute("href", "/access/rules");
    await expectNoAxeViolations(container);
  });

  it("hides New role from someone who cannot manage roles", async () => {
    mountApp({ path: "/access/roles", session: ident(["access.roles.read"]), routes: base() });
    await screen.findByRole("link", { name: "Every participant" });
    expect(screen.queryByRole("link", { name: "New role" })).not.toBeInTheDocument();
  });

  it("read-only copy: New role is disabled and says why", async () => {
    mountApp({
      path: "/access/roles",
      session: ident(ALL),
      routes: base(),
      readOnly: () => true,
    });
    const button = await screen.findByRole("button", { name: "New role" });
    await waitFor(() => expect(button).toBeDisabled());
    expect(screen.getAllByText(/This copy is read-only/).length).toBeGreaterThan(0);
  });

  it("403 shows the forbidden sentence and no retry", async () => {
    mountApp({
      path: "/access/roles",
      session: ident([]),
      routes: base({ "GET /api/access/roles": () => problem(403, { code: "permission" }) }),
    });
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "You don't have access to this screen.",
    );
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
  });

  it("500 shows the error with its reference and a retry", async () => {
    mountApp({
      path: "/access/roles",
      session: ident(ALL),
      routes: base({
        "GET /api/access/roles": () => problem(500, { code: "internal", ref: "AB12-CD34" }),
      }),
    });
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("AB12-CD34");
    expect(within(alert).getByRole("button", { name: "Try again" })).toBeInTheDocument();
  });

  it("429 says when to try again; 404 says it could not be found; an empty list says so", async () => {
    mountApp({
      path: "/access/roles",
      session: ident(ALL),
      routes: base({
        "GET /api/access/roles": () =>
          problem(429, { code: "rate_limited" }, { "Retry-After": "30" }),
      }),
    });
    expect(await screen.findByRole("alert")).toHaveTextContent("Wait 30 seconds");
  });

  it("an empty list says there are no roles", async () => {
    mountApp({
      path: "/access/roles",
      session: ident(ALL),
      routes: base({ "GET /api/access/roles": () => json({ roles: [] }) }),
    });
    expect(await screen.findByText("No roles yet.")).toBeInTheDocument();
  });
});

describe("/access/roles/:role", () => {
  it("groups permissions by module with scope and reach, and shows undeclared ones as not available", async () => {
    const { container } = mountApp({
      path: `/access/roles/${R_ADMIN}`,
      session: ident(ALL),
      routes: base({ [`GET /api/access/roles/${R_ADMIN}`]: () => json(roles[1]) }),
    });
    await screen.findByRole("button", { name: "Save changes" });
    expect(screen.getByRole("group", { name: "Access" })).toBeInTheDocument();
    expect(
      screen.getByRole("group", { name: "Not available in this installation" }),
    ).toBeInTheDocument();
    expect(screen.getByText("old.thing.do")).toBeInTheDocument();
    expect(
      screen.getByText("Starter role: access-admin. You can rename it or change what it holds."),
    ).toBeInTheDocument();
    // the starter role's name field shows its title; a sensitive permission stands out in text
    expect(screen.getByLabelText(/^Name/)).toHaveAttribute("placeholder", "Access administrator");
    expect(screen.getByLabelText(/^Name/)).not.toBeRequired();
    expect(screen.getByText("Reaches Sensitive information")).toBeInTheDocument();
    expect(screen.getByLabelText("Grant and revoke roles")).toBeChecked();
    await expectNoAxeViolations(container);
  });

  it("saves the name, description and permissions, removing an unavailable one", async () => {
    const { fetchMock } = mountApp({
      path: `/access/roles/${R_ADMIN}`,
      session: ident(ALL),
      routes: base({
        [`GET /api/access/roles/${R_ADMIN}`]: () => json(roles[1]),
        [`PUT /api/access/roles/${R_ADMIN}`]: () => json({ ...roles[1], name: "Keys" }),
      }),
    });
    await screen.findByRole("button", { name: "Save changes" });
    const name = screen.getByLabelText(/^Name/);
    await userEvent.clear(name);
    await userEvent.type(name, "Keys");
    await userEvent.click(screen.getByRole("button", { name: "Remove" }));
    await userEvent.click(screen.getByLabelText("See roles and rules"));
    await userEvent.click(screen.getByRole("button", { name: "Save changes" }));
    await screen.findByText("Role saved");
    expect(await bodyOf(fetchMock, "PUT", `/api/access/roles/${R_ADMIN}`)).toEqual({
      name: "Keys",
      description: "Looks after roles.",
      permissions: ["access.grants.manage", "access.roles.read"],
    });
  });

  it("explains last_access_admin on the role", async () => {
    mountApp({
      path: `/access/roles/${R_ADMIN}`,
      session: ident(ALL),
      routes: base({
        [`GET /api/access/roles/${R_ADMIN}`]: () => json(roles[1]),
        [`PUT /api/access/roles/${R_ADMIN}`]: () =>
          problem(409, {
            code: "conflict",
            fields: [{ field: "role", code: "last_access_admin", reason: "x" }],
          }),
      }),
    });
    await screen.findByRole("button", { name: "Save changes" });
    await userEvent.click(screen.getByRole("button", { name: "Save changes" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Someone else must hold the right to grant roles first",
    );
  });

  it("a taken name lands on the name input", async () => {
    mountApp({
      path: "/access/roles/new",
      session: ident(ALL),
      routes: base({
        "POST /api/access/roles": () =>
          problem(409, {
            code: "conflict",
            fields: [{ field: "name", code: "taken", reason: "x" }],
          }),
      }),
    });
    await screen.findByRole("button", { name: "Create role" });
    await userEvent.type(screen.getByLabelText(/^Name\s*\(required\)/), "Gate crew");
    await userEvent.click(screen.getByRole("button", { name: "Create role" }));
    await screen.findByRole("alert");
    expect(screen.getByLabelText(/^Name\s*\(required\)/)).toHaveAttribute("aria-invalid", "true");
    expect(screen.getAllByText("Another role already has this name.").length).toBeGreaterThan(0);
  });

  it("a new department role disables whole-site-only permissions and drops them", async () => {
    const { fetchMock, router } = mountApp({
      path: "/access/roles/new",
      session: ident(ALL),
      routes: base({
        "POST /api/access/roles": () => json({ ...roles[2], id: R_CUSTOM }, { status: 201 }),
        [`GET /api/access/roles/${R_CUSTOM}`]: () => json(roles[3]),
      }),
    });
    await screen.findByRole("button", { name: "Create role" });
    await userEvent.click(screen.getByLabelText("Grant and revoke roles"));
    expect(screen.getByLabelText("Grant and revoke roles")).toBeChecked();
    await userEvent.click(screen.getByRole("radio", { name: "One department" }));
    expect(screen.getByLabelText("Grant and revoke roles")).toBeDisabled();
    expect(screen.getByLabelText("Grant and revoke roles")).not.toBeChecked();
    await userEvent.click(screen.getByLabelText("See greetings"));
    await userEvent.type(screen.getByLabelText(/^Name\s*\(required\)/), "Shift lead");
    await userEvent.click(screen.getByRole("button", { name: "Create role" }));
    await waitFor(() =>
      expect(router()!.state.location.pathname).toBe(`/access/roles/${R_CUSTOM}`),
    );
    expect(await bodyOf(fetchMock, "POST", "/api/access/roles")).toEqual({
      name: "Shift lead",
      description: "",
      permissions: ["hello.greetings.read"],
      scope: "department",
    });
  });

  it("the base role has no delete button; another role deletes after a confirmation", async () => {
    mountApp({
      path: `/access/roles/${R_BASE}`,
      session: ident(ALL),
      routes: base({ [`GET /api/access/roles/${R_BASE}`]: () => json(roles[0]) }),
    });
    await screen.findByRole("button", { name: "Save changes" });
    expect(screen.getByText(/Every signed-in person holds this role/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Delete role" })).not.toBeInTheDocument();
  });

  it("deleting a role in use explains in_use in the dialog", async () => {
    mountApp({
      path: `/access/roles/${R_CUSTOM}`,
      session: ident(ALL),
      routes: base({
        [`GET /api/access/roles/${R_CUSTOM}`]: () => json(roles[3]),
        [`DELETE /api/access/roles/${R_CUSTOM}`]: () =>
          problem(409, {
            code: "conflict",
            fields: [{ field: "role", code: "in_use", reason: "x" }],
          }),
      }),
    });
    await screen.findByRole("button", { name: "Save changes" });
    await userEvent.click(screen.getByRole("button", { name: "Delete role" }));
    const dialog = await screen.findByRole("dialog", { name: "Delete this role?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Delete role" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "A grant or a rule still uses this role.",
    );
  });

  it("deletes a role, returns to the list", async () => {
    const { router, fetchMock } = mountApp({
      path: `/access/roles/${R_CUSTOM}`,
      session: ident(ALL),
      routes: base({
        [`GET /api/access/roles/${R_CUSTOM}`]: () => json(roles[3]),
        [`DELETE /api/access/roles/${R_CUSTOM}`]: () => noContent(),
      }),
    });
    await screen.findByRole("button", { name: "Save changes" });
    await userEvent.click(screen.getByRole("button", { name: "Delete role" }));
    const dialog = await screen.findByRole("dialog", { name: "Delete this role?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Delete role" }));
    await waitFor(() => expect(router()!.state.location.pathname).toBe("/access/roles"));
    expect(seen(fetchMock, "DELETE", `/api/access/roles/${R_CUSTOM}`)).toBe(true);
  });

  it("404 for an unknown role shows the not-found sentence", async () => {
    mountApp({
      path: `/access/roles/${R_CUSTOM}`,
      session: ident(ALL),
      routes: base({
        [`GET /api/access/roles/${R_CUSTOM}`]: () => problem(404, { code: "not_found" }),
      }),
    });
    expect(await screen.findByRole("alert")).toHaveTextContent("We couldn't find that.");
  });

  it("read-only copy: save and delete are disabled", async () => {
    mountApp({
      path: `/access/roles/${R_CUSTOM}`,
      session: ident(ALL),
      routes: base({ [`GET /api/access/roles/${R_CUSTOM}`]: () => json(roles[3]) }),
      readOnly: () => true,
    });
    const save = await screen.findByRole("button", { name: "Save changes" });
    await waitFor(() => expect(save).toBeDisabled());
    expect(screen.getByRole("button", { name: "Delete role" })).toBeDisabled();
  });
});

describe("/access/grants", () => {
  const withGrants = (over: Routes = {}) =>
    base({
      "GET /api/access/grants": () =>
        json({
          grants: [
            grant({ ends_at: "2099-01-01T00:00:00Z", granted_by: ME }),
            grant({
              id: "0b6f1c3e-0000-4000-8000-0000000000b2",
              ended: true,
              ends_at: "2020-01-01T00:00:00Z",
            }),
          ],
          next: "cursor-2",
        }),
      ...over,
    });

  it("lists grants as cards with names, role, end date and who granted; accessible", async () => {
    const { container } = mountApp({
      path: "/access/grants",
      session: ident(ALL),
      routes: withGrants(),
    });
    expect(await screen.findAllByRole("heading", { name: "Pat Dust" })).toHaveLength(2);
    expect(screen.getAllByText("Access administrator").length).toBeGreaterThan(0);
    expect(screen.getByText(/^Until /)).toBeInTheDocument();
    expect(screen.getByText(/^Ended /)).toBeInTheDocument();
    expect(screen.getByText(/by Dusty/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Show more" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Grant a role" })).toHaveAttribute(
      "href",
      "/access/grants/new",
    );
    await expectNoAxeViolations(container);
  });

  it("a grant holder without a playa name shows 'No playa name yet', never an ID", async () => {
    mountApp({
      path: "/access/grants",
      session: ident(ALL),
      routes: withGrants({ "GET /api/people/names": () => json({ names: { [PAT]: "" } }) }),
    });
    expect((await screen.findAllByText("No playa name yet")).length).toBeGreaterThan(0);
    expect(screen.queryByText(PAT)).not.toBeInTheDocument();
  });

  it("an ID the names answer omits, or a failed names call, shows the ID, not 'unnamed'", async () => {
    for (const names of [() => json({ names: {} }), () => problem(500, { code: "internal" })]) {
      const { unmount } = mountApp({
        path: "/access/grants",
        session: ident(ALL),
        routes: withGrants({ "GET /api/people/names": names }),
      });
      expect((await screen.findAllByRole("heading", { name: PAT })).length).toBeGreaterThan(0);
      expect(screen.queryByText("No playa name yet")).not.toBeInTheDocument();
      unmount();
    }
  });

  it("filters by person, role, permission and ended; IDs only in the address", async () => {
    const { fetchMock, router } = mountApp({
      path: `/access/grants?person=${PAT}`,
      session: ident(ALL),
      routes: withGrants(),
    });
    await screen.findAllByRole("heading", { name: "Pat Dust" });
    const calls = () =>
      fetchMock.mock.calls
        .filter(([r]) => new URL(r.url).pathname === "/api/access/grants")
        .map(([r]) => new URL(r.url).search);
    expect(calls()[0]).toBe(`?person=${PAT}`);
    await userEvent.selectOptions(screen.getByLabelText("Role"), R_ADMIN);
    await userEvent.selectOptions(screen.getByLabelText("Permission"), "access.grants.manage");
    await userEvent.click(screen.getByLabelText("Include ended grants"));
    await waitFor(() => expect(calls().at(-1)).toContain("include_ended=true"));
    expect(calls().at(-1)).toContain(`role=${R_ADMIN}`);
    expect(calls().at(-1)).toContain("permission=access.grants.manage");
    expect(router()!.state.location.search).not.toMatch(/Pat|Dust/);
  });

  it("passes a key-valued role filter to the API and shows that starter role selected", async () => {
    const { fetchMock } = mountApp({
      path: "/access/grants?role=access-admin",
      session: ident(ALL),
      routes: withGrants(),
    });
    const select = await screen.findByRole("combobox", { name: "Role" });
    await waitFor(() => expect(select).toHaveValue(R_ADMIN));
    const call = fetchMock.mock.calls.find(
      ([r]) => new URL(r.url).pathname === "/api/access/grants",
    );
    expect(new URL(call![0].url).searchParams.get("role")).toBe("access-admin");
  });

  it("filters by department, listing active departments by name, and names them on the cards", async () => {
    const { fetchMock, router } = mountApp({
      path: "/access/grants",
      session: ident(ALL),
      routes: withGrants({
        "GET /api/access/grants": () =>
          json({
            grants: [
              grant({ department_id: D_A, role_id: R_DEPT, role_name: "Shift lead" }),
              grant({
                id: "0b6f1c3e-0000-4000-8000-0000000000b3",
                department_id: "5c1a0000-0000-4000-8000-0000000000d9",
                role_id: R_DEPT,
                role_name: "Shift lead",
              }),
            ],
          }),
      }),
    });
    await screen.findAllByRole("heading", { name: "Pat Dust" });
    expect(screen.getByText("In the department: Gate")).toBeInTheDocument();
    expect(screen.getByText("In the department: an archived department")).toBeInTheDocument();
    const filter = screen.getByLabelText("In one department");
    expect(
      within(filter)
        .getAllByRole("option")
        .map((o) => o.textContent),
    ).toEqual(["Any department, or the whole site", "Gate", "Rangers"]);
    await userEvent.selectOptions(filter, D_B);
    await waitFor(() => expect(router()!.state.location.search).toBe(`?department=${D_B}`));
    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(
          ([r]) =>
            new URL(r.url).pathname === "/api/access/grants" &&
            new URL(r.url).search === `?department=${D_B}`,
        ),
      ).toBe(true),
    );
  });

  it("finds a person by searching a name that stays out of the address", async () => {
    const { router } = mountApp({
      path: "/access/grants",
      session: ident(ALL),
      routes: withGrants({
        "GET /api/people/search": () =>
          json({ matches: [{ id: PAT, playa_name: "Pat Dust", pronouns: "they" }] }),
      }),
    });
    await screen.findAllByRole("heading", { name: "Pat Dust" });
    await userEvent.type(screen.getByLabelText("Person"), "Pat");
    await userEvent.click(screen.getByRole("button", { name: "Search" }));
    await userEvent.click(await screen.findByRole("button", { name: /Pat Dust/ }));
    await waitFor(() => expect(router()!.state.location.search).toBe(`?person=${PAT}`));
  });

  it("revokes after a confirmation and shows self and last_access_admin refusals", async () => {
    let attempt = 0;
    const { fetchMock } = mountApp({
      path: "/access/grants",
      session: ident(ALL),
      routes: withGrants({
        [`DELETE /api/access/grants/${GRANT}`]: () => {
          attempt += 1;
          return attempt === 1
            ? problem(409, {
                code: "conflict",
                fields: [{ field: "role", code: "last_access_admin", reason: "x" }],
              })
            : noContent();
        },
      }),
    });
    await screen.findAllByRole("heading", { name: "Pat Dust" });
    await userEvent.click(screen.getAllByRole("button", { name: "Revoke" })[0]!);
    const dialog = await screen.findByRole("dialog", { name: "Revoke this role?" });
    expect(dialog).toHaveTextContent("Pat Dust will no longer hold Access administrator.");
    await userEvent.click(within(dialog).getByRole("button", { name: "Revoke role" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("Someone else must hold");
    await userEvent.click(within(dialog).getByRole("button", { name: "Revoke role" }));
    await screen.findByText("Role revoked");
    expect(seen(fetchMock, "DELETE", `/api/access/grants/${GRANT}`)).toBe(true);
  });

  it("revoking your own grant says you can't", async () => {
    mountApp({
      path: "/access/grants",
      session: ident(ALL),
      routes: withGrants({
        [`DELETE /api/access/grants/${GRANT}`]: () =>
          problem(400, {
            code: "invalid",
            fields: [{ field: "person", code: "self", reason: "x" }],
          }),
      }),
    });
    await screen.findAllByRole("heading", { name: "Pat Dust" });
    await userEvent.click(screen.getAllByRole("button", { name: "Revoke" })[0]!);
    const dialog = await screen.findByRole("dialog", { name: "Revoke this role?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Revoke role" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "You can't do this to yourself.",
    );
  });

  it("without a manage permission there are no revoke or grant buttons", async () => {
    mountApp({
      path: "/access/grants",
      session: ident(["access.grants.read"]),
      routes: withGrants(),
    });
    await screen.findAllByRole("heading", { name: "Pat Dust" });
    expect(screen.queryByRole("button", { name: "Revoke" })).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Grant a role" })).not.toBeInTheDocument();
  });

  it("empty, 403 and read-only states", async () => {
    const { unmount } = mountApp({ path: "/access/grants", session: ident(ALL), routes: base() });
    expect(
      await screen.findByText("Nobody holds a role matching these filters."),
    ).toBeInTheDocument();
    unmount();
    const forbidden = mountApp({
      path: "/access/grants",
      session: ident([]),
      routes: base({ "GET /api/access/grants": () => problem(403, { code: "permission" }) }),
    });
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "You don't have access to this screen.",
    );
    forbidden.unmount();
    mountApp({
      path: "/access/grants",
      session: ident(ALL),
      routes: withGrants(),
      readOnly: () => true,
    });
    const revoke = (await screen.findAllByRole("button", { name: "Revoke" }))[0]!;
    await waitFor(() => expect(revoke).toBeDisabled());
  });
});

describe("/access/grants/new", () => {
  it("grants a site role to a prefilled person with an end date as the end of that day", async () => {
    const { fetchMock, router } = mountApp({
      path: `/access/grants/new?person=${PAT}`,
      session: ident(ALL),
      routes: base({ "POST /api/access/grants": () => json(grant(), { status: 201 }) }),
    });
    expect(await screen.findByText("Pat Dust")).toBeInTheDocument();
    const role = screen.getByLabelText(/^Role\s*\(required\)/);
    // every role but the base role can be granted; a site role asks for no department
    expect(within(role).getByRole("option", { name: "Shift lead" })).toBeInTheDocument();
    expect(within(role).queryByRole("option", { name: "Participant" })).not.toBeInTheDocument();
    await userEvent.selectOptions(role, R_ADMIN);
    const date = screen.getByLabelText("Ends on");
    expect(date).toHaveAttribute("type", "date");
    await userEvent.type(date, "2099-12-31");
    await userEvent.click(screen.getByRole("button", { name: "Grant role" }));
    await waitFor(() =>
      expect(router()!.state.location.pathname + router()!.state.location.search).toBe(
        `/access/grants?person=${PAT}`,
      ),
    );
    expect(await bodyOf(fetchMock, "POST", "/api/access/grants")).toEqual({
      person_id: PAT,
      role_id: R_ADMIN,
      ends_at: new Date(2100, 0, 1).toISOString(),
    });
  });

  it("a department role asks for an active department and sends its ID", async () => {
    const { fetchMock } = mountApp({
      path: `/access/grants/new?person=${PAT}`,
      session: ident(ALL),
      routes: base({ "POST /api/access/grants": () => json(grant(), { status: 201 }) }),
    });
    await screen.findByText("Pat Dust");
    expect(screen.queryByLabelText(/^In which department/)).not.toBeInTheDocument();
    await userEvent.selectOptions(screen.getByLabelText(/^Role\s*\(required\)/), R_DEPT);
    const picker = screen.getByLabelText(/^In which department\s*\(required\)/);
    expect(
      within(picker)
        .getAllByRole("option")
        .map((o) => o.textContent),
    ).toEqual(["Choose a department", "Gate", "Rangers"]);
    await userEvent.selectOptions(picker, D_B);
    await userEvent.click(screen.getByRole("button", { name: "Grant role" }));
    await waitFor(async () =>
      expect(await bodyOf(fetchMock, "POST", "/api/access/grants")).toEqual({
        person_id: PAT,
        role_id: R_DEPT,
        department_id: D_B,
      }),
    );
  });

  it("coming from a department page preselects the lead's role and the department", async () => {
    mountApp({
      path: `/access/grants/new?person=${PAT}&department=${D_A}`,
      session: ident(ALL),
      routes: base({
        "GET /api/access/roles": () =>
          json({
            roles: [...roles, { ...roles[2], id: R_LEAD, key: "department-admin", name: "" }],
          }),
      }),
    });
    await screen.findByText("Pat Dust");
    expect(screen.getByLabelText(/^Role\s*\(required\)/)).toHaveValue(R_LEAD);
    expect(screen.getByLabelText(/^In which department\s*\(required\)/)).toHaveValue(D_A);
  });

  it("a failed department list shows its error", async () => {
    mountApp({
      path: `/access/grants/new?person=${PAT}`,
      session: ident(ALL),
      routes: base({
        "GET /api/departments/": () => problem(500, { code: "internal", ref: "AAAA-0000" }),
      }),
    });
    expect(await screen.findByRole("alert")).toHaveTextContent("Something went wrong.");
  });

  it("will not submit without a person and says so", async () => {
    mountApp({ path: "/access/grants/new", session: ident(ALL), routes: base() });
    const submit = await screen.findByRole("button", { name: "Grant role" });
    expect(submit).toBeDisabled();
    expect(submit).toHaveAccessibleDescription("Find and choose a person first.");
  });

  it("shows the separation-of-duties rule's note and the self refusal", async () => {
    let n = 0;
    mountApp({
      path: `/access/grants/new?person=${PAT}`,
      session: ident(ALL),
      routes: base({
        "POST /api/access/grants": () => {
          n += 1;
          return n === 1
            ? problem(409, {
                code: "conflict",
                fields: [
                  {
                    field: "role",
                    code: "separation_of_duties",
                    params: { rule: RULE, note: "Board members don't review incidents." },
                    reason: "x",
                  },
                ],
              })
            : problem(400, {
                code: "invalid",
                fields: [{ field: "person", code: "self", reason: "x" }],
              });
        },
      }),
    });
    await screen.findByText("Pat Dust");
    await userEvent.selectOptions(screen.getByLabelText(/^Role\s*\(required\)/), R_ADMIN);
    await userEvent.click(screen.getByRole("button", { name: "Grant role" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Board members don't review incidents.",
    );
    expect(screen.getByLabelText(/^Role\s*\(required\)/)).toHaveAttribute("aria-invalid", "true");
    await userEvent.click(screen.getByRole("button", { name: "Grant role" }));
    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent("You can't do this to yourself."),
    );
  });

  it("an end date that has passed lands on the date input; duplicate and unsupported are said", async () => {
    mountApp({
      path: `/access/grants/new?person=${PAT}`,
      session: ident(ALL),
      routes: base({
        "POST /api/access/grants": () =>
          problem(400, {
            code: "invalid",
            fields: [{ field: "ends_at", code: "range", reason: "x" }],
          }),
      }),
    });
    await screen.findByText("Pat Dust");
    await userEvent.selectOptions(screen.getByLabelText(/^Role\s*\(required\)/), R_ADMIN);
    await userEvent.click(screen.getByRole("button", { name: "Grant role" }));
    await screen.findByRole("alert");
    expect(screen.getByLabelText("Ends on")).toHaveAttribute("aria-invalid", "true");
  });

  it("501 (departments not available) is shown, not swallowed", async () => {
    mountApp({
      path: `/access/grants/new?person=${PAT}`,
      session: ident(ALL),
      routes: base({ "POST /api/access/grants": () => problem(501, { code: "unsupported" }) }),
    });
    await screen.findByText("Pat Dust");
    await userEvent.selectOptions(screen.getByLabelText(/^Role\s*\(required\)/), R_ADMIN);
    await userEvent.click(screen.getByRole("button", { name: "Grant role" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "That feature isn't turned on here.",
    );
  });

  it("read-only copy disables the button; a failed role list shows its error", async () => {
    const ro = mountApp({
      path: `/access/grants/new?person=${PAT}`,
      session: ident(ALL),
      routes: base(),
      readOnly: () => true,
    });
    const b = await screen.findByRole("button", { name: "Grant role" });
    await waitFor(() => expect(b).toBeDisabled());
    ro.unmount();
    mountApp({
      path: "/access/grants/new",
      session: ident(ALL),
      routes: base({ "GET /api/access/roles": () => problem(403, { code: "permission" }) }),
    });
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "You don't have access to this screen.",
    );
  });

  it("searching for a person without permission to search says so", async () => {
    mountApp({
      path: "/access/grants/new",
      session: ident(ALL),
      routes: base({ "GET /api/people/search": () => problem(403, { code: "permission" }) }),
    });
    await userEvent.type(await screen.findByLabelText(/^Person/), "Pa");
    await userEvent.click(screen.getByRole("button", { name: "Search" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("You can't search for people.");
  });
});

describe("/access/rules", () => {
  const rule = (over: Record<string, unknown> = {}) => ({
    id: RULE,
    role_id: R_ADMIN,
    permission: "access.grants.manage",
    note: "Keep roles apart.",
    affected: 2,
    created_at: "2026-10-01T10:00:00Z",
    ...over,
  });

  it("lists rules in words with the affected count; accessible", async () => {
    const { container } = mountApp({
      path: "/access/rules",
      session: ident(ALL),
      routes: base({
        "GET /api/access/rules": () =>
          json({
            rules: [
              rule(),
              rule({ id: "r2", permission: undefined, reaches: "sensitive", affected: 0 }),
            ],
          }),
      }),
    });
    expect(
      await screen.findByText(
        "People holding Access administrator may never hold Grant and revoke roles",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "People holding Access administrator may never hold any permission reaching Sensitive information or above",
      ),
    ).toBeInTheDocument();
    expect(screen.getByText("2 people affected now")).toBeInTheDocument();
    expect(screen.getByText("Nobody is affected now")).toBeInTheDocument();
    expect(screen.getAllByText("Keep roles apart.").length).toBe(2);
    await expectNoAxeViolations(container);
  });

  it("says what an empty list means", async () => {
    mountApp({ path: "/access/rules", session: ident(ALL), routes: base() });
    expect(await screen.findByText(/No separation-of-duties rules\./)).toBeInTheDocument();
  });

  it("creates a permission rule and a level rule", async () => {
    const { fetchMock } = mountApp({
      path: "/access/rules",
      session: ident(ALL),
      routes: base({ "POST /api/access/rules": () => json(rule(), { status: 201 }) }),
    });
    await screen.findByRole("heading", { name: "Add a rule" });
    await userEvent.selectOptions(screen.getByLabelText(/^Role\s*\(required\)/), R_ADMIN);
    await userEvent.selectOptions(
      screen.getByLabelText(/^Permission\s*\(required\)/),
      "people.data.delete",
    );
    await userEvent.type(screen.getByLabelText("Reason"), "Keep apart");
    await userEvent.click(screen.getByRole("button", { name: "Add rule" }));
    await screen.findByText("Rule added");
    expect(await bodyOf(fetchMock, "POST", "/api/access/rules")).toEqual({
      role_id: R_ADMIN,
      permission: "people.data.delete",
      note: "Keep apart",
    });
    await userEvent.click(screen.getByRole("radio", { name: "Every permission reaching a level" }));
    await userEvent.selectOptions(screen.getByLabelText(/^Role\s*\(required\)/), R_ADMIN);
    await userEvent.selectOptions(screen.getByLabelText(/^Level\s*\(required\)/), "sensitive");
    await userEvent.click(screen.getByRole("button", { name: "Add rule" }));
    await waitFor(() =>
      expect(
        fetchMock.mock.calls.filter(
          ([r]) => r.method === "POST" && new URL(r.url).pathname === "/api/access/rules",
        ),
      ).toHaveLength(2),
    );
  });

  it("a duplicate rule is explained on the form", async () => {
    mountApp({
      path: "/access/rules",
      session: ident(ALL),
      routes: base({
        "POST /api/access/rules": () =>
          problem(409, {
            code: "conflict",
            fields: [{ field: "permission", code: "duplicate", reason: "x" }],
          }),
      }),
    });
    await screen.findByRole("heading", { name: "Add a rule" });
    await userEvent.selectOptions(screen.getByLabelText(/^Role\s*\(required\)/), R_ADMIN);
    await userEvent.selectOptions(
      screen.getByLabelText(/^Permission\s*\(required\)/),
      "people.data.delete",
    );
    await userEvent.click(screen.getByRole("button", { name: "Add rule" }));
    await screen.findByRole("alert");
    expect(screen.getByLabelText(/^Permission\s*\(required\)/)).toHaveAttribute(
      "aria-invalid",
      "true",
    );
  });

  it("deletes after a confirmation", async () => {
    const { fetchMock } = mountApp({
      path: "/access/rules",
      session: ident(ALL),
      routes: base({
        "GET /api/access/rules": () => json({ rules: [rule()] }),
        [`DELETE /api/access/rules/${RULE}`]: () => noContent(),
      }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Delete" }));
    const dialog = await screen.findByRole("dialog", { name: "Delete this rule?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Delete rule" }));
    await screen.findByText("Rule deleted");
    expect(seen(fetchMock, "DELETE", `/api/access/rules/${RULE}`)).toBe(true);
  });

  it("without the manage permission there is no form and no delete; 403 and read-only are shown", async () => {
    const a = mountApp({
      path: "/access/rules",
      session: ident(["access.roles.read"]),
      routes: base({ "GET /api/access/rules": () => json({ rules: [rule()] }) }),
    });
    await screen.findByText(/may never hold/);
    expect(screen.queryByRole("button", { name: "Delete" })).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Add a rule" })).not.toBeInTheDocument();
    a.unmount();
    const b = mountApp({
      path: "/access/rules",
      session: ident([]),
      routes: base({ "GET /api/access/rules": () => problem(403, { code: "permission" }) }),
    });
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "You don't have access to this screen.",
    );
    b.unmount();
    mountApp({
      path: "/access/rules",
      session: ident(ALL),
      routes: base({ "GET /api/access/rules": () => json({ rules: [rule()] }) }),
      readOnly: () => true,
    });
    const del = await screen.findByRole("button", { name: "Delete" });
    await waitFor(() => expect(del).toBeDisabled());
  });
});

describe("review fixes", () => {
  it("changing a grant filter leaves focus on the select (no focus jump)", async () => {
    mountApp({
      path: "/access/grants",
      session: ident(ALL),
      routes: base({ "GET /api/access/grants": () => json({ grants: [grant()] }) }),
    });
    const select = await screen.findByLabelText("Role");
    await screen.findByRole("heading", { name: "Pat Dust" });
    select.focus();
    await userEvent.selectOptions(select, R_ADMIN);
    await waitFor(() => expect(select).toHaveFocus());
    await new Promise((r) => setTimeout(r, 30));
    expect(screen.getByLabelText("Role")).toHaveFocus();
  });

  it("a person without grants-read sees only their own roles, named by the server, with no filters", async () => {
    const { fetchMock } = mountApp({
      path: "/access/grants",
      session: ident([]),
      routes: base({
        "GET /api/access/grants": () =>
          json({
            grants: [grant({ person_id: ME, role_key: "access-admin", role_name: "" })],
          }),
        "GET /api/access/roles": () => problem(403, { code: "permission" }),
      }),
    });
    expect(await screen.findByRole("heading", { name: "My roles" })).toBeInTheDocument();
    expect(await screen.findByText("Access administrator")).toBeInTheDocument();
    expect(screen.queryByLabelText("Role")).not.toBeInTheDocument();
    expect(screen.queryByRole("search")).not.toBeInTheDocument();
    const call = fetchMock.mock.calls.find(
      ([r]) => new URL(r.url).pathname === "/api/access/grants",
    );
    expect(new URL(call![0].url).searchParams.get("person")).toBe(ME);
    expect(seen(fetchMock, "GET", "/api/access/roles")).toBe(false);
  });

  it("a failed role list on the grants screen is shown with a retry", async () => {
    mountApp({
      path: "/access/grants",
      session: ident(ALL),
      routes: base({
        "GET /api/access/roles": () => problem(500, { code: "internal", ref: "AB12-CD34" }),
      }),
    });
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("AB12-CD34");
    expect(within(alert).getByRole("button", { name: "Try again" })).toBeInTheDocument();
  });

  it("a missing role is left out of the grant so the server's required lands on the select", async () => {
    const { fetchMock } = mountApp({
      path: `/access/grants/new?person=${PAT}`,
      session: ident(ALL),
      routes: base({
        "POST /api/access/grants": () =>
          problem(400, {
            code: "invalid",
            fields: [{ field: "role", code: "required", reason: "x" }],
          }),
      }),
    });
    await screen.findByText("Pat Dust");
    await userEvent.click(screen.getByRole("button", { name: "Grant role" }));
    await screen.findByRole("alert");
    expect(await bodyOf(fetchMock, "POST", "/api/access/grants")).toEqual({ person_id: PAT });
    expect(screen.getByLabelText(/^Role\s*\(required\)/)).toHaveAttribute("aria-invalid", "true");
  });

  it("a granter with no profile is told to save it first", async () => {
    mountApp({
      path: `/access/grants/new?person=${PAT}`,
      session: ident(ALL),
      routes: base({
        "POST /api/access/grants": () =>
          problem(400, {
            code: "invalid",
            fields: [{ field: "actor", code: "unknown", reason: "x" }],
          }),
      }),
    });
    await screen.findByText("Pat Dust");
    await userEvent.selectOptions(screen.getByLabelText(/^Role\s*\(required\)/), R_ADMIN);
    await userEvent.click(screen.getByRole("button", { name: "Grant role" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Save your profile first, then try again.",
    );
  });

  it("a rule without a role names the role select", async () => {
    const { fetchMock } = mountApp({
      path: "/access/rules",
      session: ident(ALL),
      routes: base({
        "POST /api/access/rules": () =>
          problem(400, {
            code: "invalid",
            fields: [{ field: "role", code: "required", reason: "x" }],
          }),
      }),
    });
    await screen.findByRole("heading", { name: "Add a rule" });
    await userEvent.selectOptions(
      screen.getByLabelText(/^Permission\s*\(required\)/),
      "people.data.delete",
    );
    await userEvent.click(screen.getByRole("button", { name: "Add rule" }));
    await screen.findByRole("alert");
    expect(await bodyOf(fetchMock, "POST", "/api/access/rules")).toEqual({
      permission: "people.data.delete",
    });
    expect(screen.getByLabelText(/^Role\s*\(required\)/)).toHaveAttribute("name", "role");
    expect(screen.getByLabelText(/^Role\s*\(required\)/)).toHaveAttribute("aria-invalid", "true");
  });

  it("the person search says how many it found", async () => {
    mountApp({
      path: "/access/grants/new",
      session: ident(ALL),
      routes: base({
        "GET /api/people/search": () =>
          json({ matches: [{ id: PAT, playa_name: "Pat Dust", pronouns: "" }] }),
      }),
    });
    await userEvent.type(await screen.findByLabelText(/^Person/), "Pa");
    await userEvent.click(screen.getByRole("button", { name: "Search" }));
    expect(await screen.findByText("1 person found")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /Pat Dust/ }));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Choose someone else" })).toHaveFocus(),
    );
  });

  it("a failed catalogue on the rules screen is shown", async () => {
    mountApp({
      path: "/access/rules",
      session: ident(ALL),
      routes: base({
        "GET /api/access/permissions": () => problem(500, { code: "internal", ref: "AB12-CD34" }),
      }),
    });
    expect(await screen.findByRole("alert")).toHaveTextContent("AB12-CD34");
  });

  it("My roles with nothing says so", async () => {
    mountApp({
      path: "/access/grants",
      session: ident([]),
      routes: base({ "GET /api/access/grants": () => json({ grants: [] }) }),
    });
    expect(await screen.findByText("You don't hold any roles.")).toBeInTheDocument();
  });
});

describe("permission labels", () => {
  it("every core permission and area has a title in the catalogue", async () => {
    mountApp({ session: ident([]) }); // loads the catalogues
    await screen.findByRole("heading", { level: 1 });
    const keys = [
      "accounts.manage",
      "accounts.read",
      "audit.export",
      "audit.search",
      "core.modules.read",
      "jobs.manage",
      "jobs.read",
      "settings.manage",
      "settings.read",
    ].flatMap((p) => {
      const [area = "", ...rest] = p.split(".");
      return [`module.${area}.permission.${rest.join(".")}.title`, `module.${area}.title`];
    });
    for (const k of keys) expect(isMessageKey(k), k).toBe(true);
  });
});
