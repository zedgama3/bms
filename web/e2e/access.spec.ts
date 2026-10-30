// End-to-end smoke run, roles, grants and rules (authz.md "UI") (web-shell.md decision 22, "Test plan").
import { expect } from "@playwright/test";
import type { Page } from "@playwright/test";
import {
  noHorizontalScroll,
  axe,
  touchTargets,
  openMenu,
  mockMe,
  press,
  test,
  only,
  PAT,
  adminSession,
} from "./support/helpers";

// ---- access (authz.md "UI"): roles, grants, rules; the server is mocked at the network ----

const R_ADMIN = "0b6f1c3e-0000-4000-8000-0000000000a2";
const R_CREW = "0b6f1c3e-0000-4000-8000-0000000000a4";
const ADMIN_PERMS = [
  "access.roles.read",
  "access.roles.manage",
  "access.rules.manage",
  "access.grants.read",
  "access.grants.manage",
];
const accessRoles = [
  {
    id: R_ADMIN,
    key: "access-admin",
    name: "",
    description: "Looks after roles and grants.",
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
    id: R_CREW,
    name: "Gate crew with a rather long role name for a narrow screen",
    description: "",
    scope: "site",
    base: false,
    permissions: [],
    created_at: "2026-10-01T10:00:00Z",
    updated_at: "2026-10-01T10:00:00Z",
  },
];
const accessCatalogue = [
  {
    name: "access.grants.manage",
    scope: "site",
    reaches: "internal",
    module: "access",
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

async function mockAccess(page: Page, permissions = ADMIN_PERMS) {
  await page.route("**/api/accounts/session", (r) =>
    r.fulfill({ json: adminSession(permissions) }),
  );
  await mockMe(page);
  await page.route("**/api/access/roles", (r) =>
    r.request().method() === "GET"
      ? r.fulfill({ json: { roles: accessRoles } })
      : r.fulfill({ status: 201, json: accessRoles[1] }),
  );
  await page.route(`**/api/access/roles/${R_ADMIN}`, (r) => r.fulfill({ json: accessRoles[0] }));
  await page.route("**/api/access/permissions", (r) =>
    r.fulfill({ json: { permissions: accessCatalogue } }),
  );
  await page.route("**/api/access/rules", (r) =>
    r.request().method() === "GET"
      ? r.fulfill({
          json: {
            rules: [
              {
                id: "0b6f1c3e-0000-4000-8000-0000000000c1",
                role_id: R_ADMIN,
                permission: "people.data.delete",
                note: "Keep roles and deletion apart.",
                affected: 1,
                created_at: "2026-10-01T10:00:00Z",
              },
            ],
          },
        })
      : r.fulfill({ status: 201, json: {} }),
  );
  await page.route(/\/api\/access\/grants(\?.*)?$/, (r) =>
    r.fulfill({
      json: {
        grants: [
          {
            id: "0b6f1c3e-0000-4000-8000-0000000000b1",
            person_id: PAT,
            role_id: R_ADMIN,
            role_key: "access-admin",
            role_name: "",
            ends_at: "2099-01-01T00:00:00Z",
            created_at: "2026-10-02T10:00:00Z",
            ended: false,
          },
        ],
      },
    }),
  );
  // the grant screens read the active departments (filter and picker)
  await page.route(/\/api\/departments\/?(\?.*)?$/, (r) =>
    r.fulfill({ json: { departments: [] } }),
  );
  await page.route("**/api/people/names?**", (r) =>
    r.fulfill({ json: { names: { [PAT]: "Pat Dust" } } }),
  );
  await page.route("**/api/people/search?**", (r) =>
    r.fulfill({ json: { matches: [{ id: PAT, playa_name: "Pat Dust", pronouns: "they" }] } }),
  );
}

test.describe("access: the admin screens", () => {
  for (const scheme of ["light", "dark"] as const) {
    test(`roles, editor, grants, new grant and rules at phone width, 44 px targets, axe clean (${scheme})`, async ({
      page,
    }, info) => {
      only("default");
      const isPhone = info.project.name === "phone";
      await page.emulateMedia({ colorScheme: scheme });
      await mockAccess(page);
      if (isPhone) await page.setViewportSize({ width: 320, height: 568 });
      const check = async () => {
        if (isPhone) {
          await noHorizontalScroll(page);
          await touchTargets(page);
        }
        await axe(page);
      };

      await page.goto("/access/roles");
      await expect(page.getByRole("heading", { level: 1, name: "Roles" })).toBeVisible();
      await expect(page.getByRole("link", { name: "Access administrator" })).toBeVisible();
      await check();

      await press(page.getByRole("link", { name: "Access administrator" }), isPhone);
      await expect(page).toHaveURL(new RegExp(`/access/roles/${R_ADMIN}$`));
      await expect(page.getByRole("button", { name: "Save changes" })).toBeVisible();
      await expect(page.getByText("Not available in this installation")).toBeVisible();
      await check();

      await page.goto("/access/grants");
      await expect(page.getByRole("heading", { level: 1, name: "Grants" })).toBeVisible();
      await expect(page.getByRole("heading", { name: "Pat Dust" })).toBeVisible();
      await check();

      await press(page.getByRole("button", { name: "Revoke" }), isPhone);
      await expect(page.getByRole("dialog", { name: "Revoke this role?" })).toBeVisible();
      await check();
      await press(
        page
          .getByRole("dialog", { name: "Revoke this role?" })
          .getByRole("button", { name: "Cancel" }),
        isPhone,
      );

      await page.goto(`/access/grants/new?person=${PAT}`);
      await expect(page.getByRole("heading", { level: 1, name: "Grant a role" })).toBeVisible();
      await expect(page.getByText("Pat Dust")).toBeVisible();
      await check();

      await page.goto("/access/rules");
      await expect(
        page.getByRole("heading", { level: 1, name: /Separation-of-duties/ }),
      ).toBeVisible();
      await expect(page.getByText(/may never hold/)).toBeVisible();
      await check();
    });
  }

  test("grant a role and see the menu change", async ({ page }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    // The granter holds the grant permissions; after the grant the session the page reads is
    // the grantee's, who now holds roles.read, and the Administration menu gains Roles.
    let granted = false;
    await mockAccess(page);
    await page.route("**/api/accounts/session", (r) =>
      r.fulfill({
        json: adminSession(
          granted ? ["access.roles.read"] : ["access.grants.manage", "access.grants.read"],
        ),
      }),
    );
    let body: unknown;
    await page.route("**/api/access/grants", (r) => {
      body = r.request().postDataJSON();
      granted = true;
      return r.fulfill({
        status: 201,
        json: {
          id: "0b6f1c3e-0000-4000-8000-0000000000b1",
          person_id: PAT,
          role_id: R_ADMIN,
          created_at: "2026-10-02T10:00:00Z",
          ended: false,
        },
      });
    });
    await page.goto(`/access/grants/new?person=${PAT}`);
    await expect(page.getByText("Pat Dust")).toBeVisible();
    await page.getByLabel(/^Role/).selectOption(R_ADMIN);
    await press(page.getByRole("button", { name: "Grant role" }), isPhone);
    await expect(page.getByText("Role granted")).toBeVisible();
    expect(body).toEqual({ person_id: PAT, role_id: R_ADMIN });

    // before: no Roles entry; after the next page load the entry is there
    await page.reload();
    await openMenu(page, isPhone);
    const admin = page.getByRole("navigation", { name: "Administration" });
    await expect(admin.getByRole("link", { name: "Roles" })).toBeVisible();
  });

  test("a 403 shows the forbidden state, accessibly", async ({ page }) => {
    only("default");
    await mockAccess(page, []);
    await page.route("**/api/access/roles", (r) =>
      r.fulfill({
        status: 403,
        contentType: "application/problem+json",
        body: JSON.stringify({ status: 403, code: "permission" }),
      }),
    );
    await page.goto("/access/roles");
    await expect(page.getByRole("alert")).toContainText("You don't have access to this screen.");
    await axe(page);
  });
});
