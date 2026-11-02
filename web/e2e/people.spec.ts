// End-to-end smoke run, the signed-in person's screens, the directory, person page and deletion requests (web-shell.md decision 22, "Test plan").
import { expect } from "@playwright/test";
import {
  noHorizontalScroll,
  axe,
  touchTargets,
  openMenu,
  profileOf,
  mockMe,
  press,
  NOBODY,
  test,
  only,
  me,
  PAT,
} from "./support/helpers";

// ---- people (people.md "UI"): the server is mocked at the network ----

const REQ = "0b6f1c3e-0000-4000-8000-0000000000e1";
const contactCard = {
  id: "0b6f1c3e-0000-4000-8000-0000000000d1",
  person_id: "7d2c2f0e-0000-4000-8000-000000000001",
  name: "Sam Friend",
  phone: "555-0199",
  relationship: "friend",
  position: 1,
  created_at: "2026-10-01T10:00:00Z",
  updated_at: "2026-10-01T10:00:00Z",
};

test.describe("people: the signed-in person's screens", () => {
  test("the first-sign-in prompt, then the profile, at the phone viewport", async ({
    page,
  }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    await page.route("**/api/accounts/session", (r) =>
      r.fulfill({ json: { ...me, playa_name: "" } }),
    );
    let saved = "";
    await page.route("**/api/datarights/me/requests", (r) => r.fulfill({ json: { requests: [] } }));
    await page.route("**/api/people/me", (r) => {
      if (r.request().method() === "PUT") {
        saved = (r.request().postDataJSON() as { playa_name: string }).playa_name;
      }
      return r.fulfill({ json: profileOf(saved) });
    });
    await page.goto("/");
    await expect(page).toHaveURL(/\/people\/me\?first=1$/);
    await expect(page.getByText(/Tell us your playa name/)).toBeVisible();
    await expect(
      page.getByText(/It is permanent, and it must be one no one else here/),
    ).toBeVisible();
    await expect(page.getByLabel(/^Playa name/)).toBeFocused();
    if (isPhone) {
      await page.setViewportSize({ width: 320, height: 568 });
      await noHorizontalScroll(page);
      await touchTargets(page);
    }
    await axe(page);
    await page.getByLabel(/^Playa name/).fill("Dusty D");
    await press(page.getByRole("button", { name: "Save", exact: true }), isPhone);
    await expect(page.getByText("Saved")).toBeVisible();
    expect(saved).toBe("Dusty D");
    await expect(page.getByLabel(/^Playa name/)).toHaveAttribute("readonly", "");
    await openMenu(page, isPhone);
    await expect(page.locator("header span.font-semibold", { hasText: "Dusty D" })).toBeVisible();
  });

  for (const scheme of ["light", "dark"] as const) {
    test(`contacts, consents and your data lay out at phone width, axe clean (${scheme})`, async ({
      page,
    }, info) => {
      only("default");
      const isPhone = info.project.name === "phone";
      await page.emulateMedia({ colorScheme: scheme });
      await page.route("**/api/accounts/session", (r) => r.fulfill({ json: me }));
      await mockMe(page, "Dusty", {
        contacts: [contactCard],
        consents: [
          {
            person_id: "7d2c2f0e-0000-4000-8000-000000000001",
            purpose: "hello.newsletter",
            granted: true,
            changed_at: "2026-10-01T10:00:00Z",
          },
        ],
        sections: [{ key: "hello.unavailable", items: [], failed: true }],
      });
      await page.route("**/api/datarights/me/requests", (r) =>
        r.fulfill({ json: { requests: [] } }),
      );
      await page.route("**/api/datarights/me/export", (r) =>
        r.fulfill({
          status: 429,
          headers: { "Retry-After": "420" },
          contentType: "application/problem+json",
          body: JSON.stringify({ status: 429, code: "rate_limited" }),
        }),
      );
      if (isPhone) await page.setViewportSize({ width: 320, height: 568 });
      await page.goto("/people/me");
      await expect(page.getByRole("heading", { level: 1, name: "My profile" })).toBeVisible();
      await expect(page.getByText("Sam Friend")).toBeVisible();
      if (isPhone) {
        await noHorizontalScroll(page);
        await touchTargets(page);
      }
      await axe(page);

      await press(page.getByRole("button", { name: "Add a contact" }), isPhone);
      const dialog = page.getByRole("dialog", { name: "Add an emergency contact" });
      await expect(dialog).toBeVisible();
      if (isPhone) await touchTargets(page);
      await axe(page);
      await press(dialog.getByRole("button", { name: "Cancel" }), isPhone);

      await press(page.getByRole("button", { name: "Download my data" }), isPhone);
      await expect(page.getByRole("alert")).toContainText("Try again in 7 minutes.");
    });
  }

  test("download my data hands over a file", async ({ page }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    await page.route("**/api/accounts/session", (r) => r.fulfill({ json: me }));
    await mockMe(page);
    await page.route("**/api/datarights/me/requests", (r) => r.fulfill({ json: { requests: [] } }));
    await page.route("**/api/datarights/me/export", (r) => r.fulfill({ json: { format: 1 } }));
    await page.goto("/people/me");
    const download = page.waitForEvent("download");
    await press(page.getByRole("button", { name: "Download my data" }), isPhone);
    expect((await download).suggestedFilename()).toBe("bms-export.json");
  });

  test("request deletion and withdraw it", async ({ page }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    await page.route("**/api/accounts/session", (r) => r.fulfill({ json: me }));
    await mockMe(page);
    let open = false;
    await page.route("**/api/datarights/me/requests", (r) => {
      if (r.request().method() === "POST") {
        open = true;
        return r.fulfill({ status: 201, json: {} });
      }
      return r.fulfill({
        json: {
          requests: open
            ? [
                {
                  id: REQ,
                  person_id: "7d2c2f0e-0000-4000-8000-000000000001",
                  kind: "delete",
                  status: "open",
                  note: "",
                  created_at: "2026-10-05T10:00:00Z",
                  decision_note: "",
                },
              ]
            : [],
        },
      });
    });
    await page.route(`**/api/datarights/me/requests/${REQ}/withdraw`, (r) => {
      open = false;
      return r.fulfill({ status: 204 });
    });
    await page.goto("/people/me");
    await press(page.getByRole("button", { name: "Request deletion" }), isPhone);
    const dialog = page.getByRole("dialog", { name: "Request deletion?" });
    await expect(dialog).toBeVisible();
    if (isPhone) await touchTargets(page);
    await axe(page);
    await press(dialog.getByRole("button", { name: "Request deletion" }), isPhone);
    await expect(page.getByText(/You asked for deletion on/)).toBeVisible();
    await press(page.getByRole("button", { name: "Withdraw request" }), isPhone);
    await expect(page.getByText("Request withdrawn")).toBeVisible();
  });
});

test.describe("people: directory, person page and deletion requests", () => {
  const holder = {
    ...me,
    permissions: ["people.directory.read", "people.profiles.manage", "people.data.delete"],
  };

  test("search, open a person, see the audit link and the recorded note, at the phone viewport", async ({
    page,
  }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    await page.route("**/api/accounts/session", (r) => r.fulfill({ json: holder }));
    await mockMe(page);
    await page.route("**/api/people/search?**", (r) =>
      r.fulfill({ json: { matches: [{ id: PAT, playa_name: "Pat Dust", pronouns: "she/her" }] } }),
    );
    await page.route(`**/api/people/${PAT}`, (r) =>
      r.fulfill({
        json: profileOf("Pat Dust", {
          contacts: [contactCard],
          fields: ["playa_name", "pronouns", "legal_name", "phone", "emergency_contacts"],
          can: { ...NOBODY, read_contacts: true, view_audit: true, view_roles: true, delete: true },
        }),
      }),
    );
    if (isPhone) await page.setViewportSize({ width: 320, height: 568 });
    await page.goto("/people");
    await expect(page.getByRole("heading", { level: 1, name: "People" })).toBeVisible();
    await page.getByLabel("Find a person").fill("Pat");
    await press(page.getByRole("button", { name: "Search", exact: true }), isPhone);
    await expect(page.getByRole("link", { name: /Pat Dust/ })).toBeVisible();
    expect(new URL(page.url()).search).toBe("");
    if (isPhone) {
      await noHorizontalScroll(page);
      await touchTargets(page);
    }
    await axe(page);

    await press(page.getByRole("link", { name: /Pat Dust/ }), isPhone);
    await expect(page.getByText("Viewing these details is recorded.")).toBeVisible();
    await expect(page.getByRole("link", { name: "Audit log" })).toHaveAttribute(
      "href",
      `/audit?subject=${PAT}`,
    );
    if (isPhone) {
      await noHorizontalScroll(page);
      await touchTargets(page);
    }
    await axe(page);

    await press(page.getByRole("button", { name: "Delete person" }), isPhone);
    const dialog = page.getByRole("dialog", { name: "Delete this person?" });
    await expect(dialog).toContainText("You are about to delete Pat Dust.");
    if (isPhone) await touchTargets(page);
    await axe(page);
  });

  test("a manager renames a set playa name behind a confirm; a taken name shows inline", async ({
    page,
  }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    await page.route("**/api/accounts/session", (r) => r.fulfill({ json: holder }));
    await mockMe(page);
    await page.route(`**/api/people/${PAT}`, (r) => {
      if (r.request().method() === "PUT") {
        return r.fulfill({
          status: 409,
          contentType: "application/problem+json",
          body: JSON.stringify({
            status: 409,
            code: "conflict",
            fields: [{ field: "playa_name", code: "taken", reason: "x" }],
          }),
        });
      }
      return r.fulfill({
        json: profileOf("Pat Dust", { can: { ...NOBODY, edit: true, read_contacts: true } }),
      });
    });
    if (isPhone) await page.setViewportSize({ width: 320, height: 568 });
    await page.goto(`/people/${PAT}`);
    const name = page.getByLabel(/^Playa name/);
    await expect(name).toBeEditable();
    await name.fill("Pat Sand");
    await press(page.getByRole("button", { name: "Save", exact: true }), isPhone);
    const dialog = page.getByRole("dialog", { name: "Change this playa name?" });
    await expect(dialog).toContainText(
      "This changes a permanent playa name. The change is recorded.",
    );
    if (isPhone) await touchTargets(page);
    await axe(page);
    await press(dialog.getByRole("button", { name: "Change the name" }), isPhone);
    await expect(
      page.getByText("Someone here already uses this playa name. Choose another.").first(),
    ).toBeVisible();
    await expect(name).toHaveAttribute("aria-invalid", "true");
  });

  test("deletion requests: fulfil with a confirmation and read the report", async ({
    page,
  }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    await page.route("**/api/accounts/session", (r) => r.fulfill({ json: holder }));
    await mockMe(page);
    await page.route("**/api/datarights/requests?**", (r) =>
      r.fulfill({
        json: {
          requests: [
            {
              id: REQ,
              person_id: PAT,
              kind: "delete",
              status: "open",
              note: "Please delete me",
              created_at: "2026-10-05T10:00:00Z",
              decision_note: "",
            },
          ],
        },
      }),
    );
    await page.route("**/api/people/names?**", (r) =>
      r.fulfill({ json: { names: { [PAT]: "Pat Dust" } } }),
    );
    await page.route(`**/api/datarights/requests/${REQ}/fulfil`, (r) =>
      r.fulfill({
        json: {
          person: PAT,
          people: 1,
          identity: 1,
          access: 0,
          modules: {},
          kept: [{ kind: "audit.entry", count: 4 }],
        },
      }),
    );
    if (isPhone) await page.setViewportSize({ width: 320, height: 568 });
    await page.goto("/people/admin/requests");
    await expect(page.getByRole("heading", { level: 1, name: "Deletion requests" })).toBeVisible();
    await expect(page.getByText("Please delete me")).toBeVisible();
    if (isPhone) {
      await noHorizontalScroll(page);
      await touchTargets(page);
    }
    await axe(page);
    await press(page.getByRole("button", { name: "Fulfil" }), isPhone);
    const confirm = page.getByRole("dialog", { name: "Delete this person?" });
    await expect(confirm).toBeVisible();
    await press(confirm.getByRole("button", { name: "Delete person" }), isPhone);
    const report = page.getByRole("dialog", { name: "Person deleted" });
    await expect(report).toContainText("4 records of audit log entries kept");
    if (isPhone) await touchTargets(page);
    await axe(page);
  });

  test("without people.directory.read the directory shows the forbidden state up front", async ({
    page,
  }) => {
    only("default");
    await page.route("**/api/accounts/session", (r) =>
      r.fulfill({ json: { ...me, permissions: [] } }),
    );
    await mockMe(page);
    await page.goto("/people");
    await expect(page.getByRole("alert")).toContainText("You don't have access to this screen.");
    await expect(page.getByLabel("Find a person")).toHaveCount(0);
    await axe(page);
  });
});
