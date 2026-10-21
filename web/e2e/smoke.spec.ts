import AxeBuilder from "@axe-core/playwright";
import { expect, test as base } from "@playwright/test";
import type { Locator, Page } from "@playwright/test";

/** The public settings document the shell reads at boot (`GET /api/settings/public`). */
function publicSettings(over: Record<string, unknown> = {}) {
  return {
    name: "BMS",
    contact_email: "",
    language: "en",
    languages: ["en"],
    public_url: "",
    theme: false,
    timezone: "",
    terms: {},
    ...over,
  };
}

/** Answers the two requests the shell makes at boot for the regional's settings, so a test that
 *  mocks the session does not depend on what the real server holds. A route a test adds later
 *  takes precedence. */
async function mockPublicSettings(page: Page, doc: () => unknown = () => publicSettings()) {
  await page.route("**/api/settings/public", (r) => r.fulfill({ json: doc() }));
  await page.route("**/api/settings/theme.css**", (r) =>
    r.fulfill({ contentType: "text/css", body: "" }),
  );
}

/** Every test mocks the public settings, except the ones that talk to the real server
 *  (`test.use({ stubPublicSettings: false })`), which get the real document and theme. */
const test = base.extend<{ stubPublicSettings: boolean }>({
  stubPublicSettings: [true, { option: true }],
  page: async ({ page, stubPublicSettings }, provide) => {
    if (stubPublicSettings) await mockPublicSettings(page);
    await provide(page);
  },
});

const mode = process.env["BMS_E2E_MODE"] ?? "default";
const only = (m: string) => test.skip(mode !== m, `needs BMS_E2E_MODE=${m}`);

/** Console messages that mean the CSP blocked something. */
function watchCsp(page: Page): string[] {
  const violations: string[] = [];
  page.on("console", (m) => {
    if (/content security policy|refused to (apply|load|execute|connect)/i.test(m.text()))
      violations.push(m.text());
  });
  page.on("pageerror", (e) => violations.push(`pageerror: ${e.message}`));
  return violations;
}

async function noHorizontalScroll(page: Page) {
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  expect(overflow, "horizontal overflow in CSS px").toBeLessThanOrEqual(0);
}

async function axe(page: Page) {
  const results = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"])
    .analyze();
  expect(
    results.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`),
  ).toEqual([]);
}

/** On a phone every control is at least 44 x 44 CSS px. */
async function touchTargets(page: Page) {
  const small = await page.evaluate(() => {
    const out: string[] = [];
    for (const el of document.querySelectorAll<HTMLElement>(
      "a[href], button, input:not([type=hidden]), select, textarea, summary",
    )) {
      if (el.closest(".sr-only") || el.classList.contains("sr-only")) continue;
      const r = el.getBoundingClientRect();
      if (r.width === 0 || r.height === 0) continue;
      if (r.width < 44 || r.height < 44)
        out.push(
          `${el.tagName.toLowerCase()} "${(el.textContent ?? "").trim().slice(0, 30)}" ${Math.round(r.width)}x${Math.round(r.height)}`,
        );
    }
    return out;
  });
  expect(small, "targets under 44 x 44 px").toEqual([]);
}

/** Opens the navigation on a phone, where it is a disclosure button. */
async function openMenu(page: Page, isPhone: boolean): Promise<Locator> {
  if (isPhone) {
    const menu = page.getByRole("button", { name: "Menu" });
    await expect(menu).toHaveAttribute("aria-expanded", "false");
    await menu.tap();
    await expect(menu).toHaveAttribute("aria-expanded", "true");
  }
  return page.getByRole("navigation", { name: "Main" });
}

/** The session answer for a signed-out visitor on a copy where sign-up is open. */
async function signedOut(page: Page) {
  await page.route("**/api/accounts/session", (r) =>
    r.fulfill({ json: { signed_in: false, signup_open: true, superuser: false, permissions: [] } }),
  );
}

const NOBODY = {
  edit: false,
  read_contacts: false,
  export: false,
  delete: false,
  link: false,
  view_roles: false,
  view_audit: false,
};
const OPTIONAL = { legal_name: "optional", phone: "optional", emergency_contacts: "optional" };

/** The own-profile answer (`GET /api/people/me`) the frame reads once per session. */
function profileOf(playa: string, over: Record<string, unknown> = {}) {
  return {
    person: {
      id: "7d2c2f0e-0000-4000-8000-000000000001",
      playa_name: playa,
      pronouns: "",
      language: "en",
      has_account: true,
      email: "dusty@example.test",
      complete: playa !== "",
      created_at: "2026-10-01T10:00:00Z",
      updated_at: "2026-10-02T10:00:00Z",
    },
    fields: ["playa_name", "pronouns", "language", "legal_name", "phone", "email", "consents"],
    contacts: [],
    consents: [],
    sections: [],
    can: NOBODY,
    policy: OPTIONAL,
    ...over,
  };
}

/** Mocks the own profile for a mocked session (an unmocked call would reach the real server
 *  with a cookie it never issued and sign the page out). */
async function mockMe(page: Page, playa = "Dusty", over: Record<string, unknown> = {}) {
  await page.route("**/api/people/me", (r) => r.fulfill({ json: profileOf(playa, over) }));
  await page.route("**/api/people/me/requests", (r) => r.fulfill({ json: { requests: [] } }));
}

/** A tap on a phone, a click on the desktop: every flow must work with touch alone. */
async function press(target: Locator, isPhone: boolean) {
  if (isPhone) await target.tap();
  else await target.click();
}

test.describe("the frame", () => {
  test.use({ stubPublicSettings: false }); // the real server answers
  test("renders with a skip link and landmarks; on a phone the Menu disclosure opens and closes on navigation", async ({
    page,
  }, info) => {
    only("default");
    const csp = watchCsp(page);
    const res = await page.goto("/");
    expect(res?.headers()["content-security-policy"]).toContain("default-src 'self'");
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    await expect(page.locator("header")).toHaveCount(1);
    await expect(page.locator("main#content")).toHaveCount(1);
    await expect(page.locator("footer")).toHaveCount(1);
    await expect(page.locator("footer small")).toContainText("Version");
    const first = await page.evaluate(() =>
      document.querySelector("a, button, input, select")?.textContent?.trim(),
    );
    expect(first).toBe("Skip to content");

    const isPhone = info.project.name === "phone";
    const nav = await openMenu(page, isPhone);
    await expect(nav.getByRole("link", { name: "Hello" })).toBeVisible();
    if (isPhone) await nav.getByRole("link", { name: "Hello" }).tap();
    else await nav.getByRole("link", { name: "Hello" }).click();
    await expect(page).toHaveURL(/\/hello$/);
    if (isPhone)
      await expect(page.getByRole("button", { name: "Menu" })).toHaveAttribute(
        "aria-expanded",
        "false",
      );
    await expect(page.getByRole("heading", { level: 1 })).toBeFocused();
    expect(csp).toEqual([]);
  });

  test("the CSP header is on / and no CSP violation is logged across the shell's screens", async ({
    page,
  }) => {
    only("default");
    const csp = watchCsp(page);
    const res = await page.goto("/");
    expect(res?.headers()["content-security-policy"]).toMatch(/default-src 'self'/);
    for (const path of ["/hello", "/sign-in", "/accounts/sign-up", "/admin/modules", "/nowhere"]) {
      await page.goto(path);
      await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    }
    expect(csp).toEqual([]);
  });
});

test.describe("hello", () => {
  test.use({ stubPublicSettings: false }); // the real server answers
  test("the entry appears and its screen calls GET /api/hello/public-greeting", async ({
    page,
  }) => {
    only("default");
    const called = page.waitForResponse(
      (r) =>
        new URL(r.url()).pathname === "/api/hello/public-greeting" &&
        r.request().method() === "GET",
    );
    await page.goto("/hello");
    expect((await called).status()).toBe(200);
    await expect(page.getByRole("heading", { level: 1, name: /!$/ })).toBeVisible();
    await expect(page.getByRole("button", { name: "Say hello" })).toHaveCount(0); // M0: signed out, public part only
  });

  test("with the module disabled the entry is gone and /hello is not found", async ({ page }) => {
    only("hello-disabled");
    await page.goto("/");
    await expect(page.getByRole("link", { name: "Hello" })).toHaveCount(0);
    await page.goto("/hello");
    await expect(page.getByRole("heading", { level: 1, name: "Not found" })).toBeVisible();
  });

  test("a 401 lands on /accounts/sign-in?returnTo=/hello and shows the sign-in form", async ({
    page,
  }) => {
    only("default");
    // Force the signed-out answer through the real client: the screen's call gets a 401.
    await page.route("**/api/hello/public-greeting", (route) =>
      route.fulfill({
        status: 401,
        contentType: "application/problem+json",
        body: JSON.stringify({ status: 401, code: "unauthenticated" }),
      }),
    );
    await page.goto("/hello");
    await expect(page).toHaveURL(/\/accounts\/sign-in\?returnTo=%2Fhello$/);
    await expect(page.getByRole("heading", { level: 1, name: "Sign in" })).toBeVisible();
    await expect(page.getByLabel(/E-mail address/)).toBeVisible();
  });
});

test.describe("read-only copy", () => {
  test.use({ stubPublicSettings: false }); // the real server answers
  test("the banner appears after the first call and the server's X-Read-Only is true", async ({
    page,
  }) => {
    only("readonly");
    const modules = page.waitForResponse((r) => new URL(r.url()).pathname === "/api/modules");
    await page.goto("/");
    expect((await modules).headers()["x-read-only"]).toBe("true");
    await expect(page.getByText(/This copy is read-only/)).toBeVisible();
  });
});

const screens: [string, string][] = [
  ["home", "/"],
  ["sign-in", "/sign-in"],
  ["sign-up", "/accounts/sign-up"],
  ["hello", "/hello"],
  ["not found", "/nowhere"],
];

test.describe("accessibility and the phone layout", () => {
  test.use({ stubPublicSettings: false }); // the real server answers
  for (const scheme of ["light", "dark"] as const) {
    for (const [name, path] of screens) {
      test(`${name} has no axe violations (${scheme})`, async ({ page }) => {
        only("default");
        await page.emulateMedia({ colorScheme: scheme });
        await signedOut(page);
        await page.goto(path);
        await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
        await axe(page);
      });
    }
  }

  for (const [name, path] of screens) {
    test(`${name}: no horizontal scroll, 44 px targets and the heading in the first viewport on a phone`, async ({
      page,
    }, info) => {
      only("default");
      test.skip(info.project.name !== "phone", "the phone project only");
      await signedOut(page);
      await page.goto(path);
      await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
      await noHorizontalScroll(page);
      await touchTargets(page);
      // The first viewport shows the heading without scrolling; each screen's primary action
      // follows it directly (home: the sign-in or the cards; sign-in: the form).
      const inView = await page.evaluate(() => {
        const h1 = document.querySelector("h1")?.getBoundingClientRect();
        return !!h1 && h1.bottom <= window.innerHeight;
      });
      expect(inView).toBe(true);
    });

    test(`${name}: works at 320 px wide with 200% text`, async ({ page }, info) => {
      only("default");
      test.skip(info.project.name !== "phone", "the phone project only");
      await page.setViewportSize({ width: 320, height: 568 });
      await signedOut(page);
      await page.goto(path);
      await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
      await noHorizontalScroll(page);
      // Text at 200% (WCAG 1.4.4) still reflows. The CSP forbids an injected <style> or style
      // attribute, so the rule arrives as a same-origin stylesheet that the test serves.
      await page.route("**/e2e-zoom.css", (route) =>
        route.fulfill({ contentType: "text/css", body: "html { font-size: 200%; }" }),
      );
      await page.evaluate(
        () =>
          new Promise<void>((resolve, reject) => {
            const link = document.createElement("link");
            link.rel = "stylesheet";
            link.href = "/e2e-zoom.css";
            link.onload = () => resolve();
            link.onerror = () => reject(new Error("zoom stylesheet blocked"));
            document.head.append(link);
          }),
      );
      const fontPx = await page.evaluate(() =>
        parseFloat(getComputedStyle(document.documentElement).fontSize),
      );
      expect(fontPx).toBeGreaterThanOrEqual(30);
      await noHorizontalScroll(page);
      await touchTargets(page);
    });
  }
});

test.describe("/admin/modules (signed in, mocked at the network)", () => {
  const person = {
    signed_in: true,
    person_id: "7d2c2f0e-0000-4000-8000-000000000001",
    playa_name: "Dusty",
    superuser: false,
    permissions: ["core.modules.read"],
  };
  const body = {
    version: "dev",
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
      ],
      edges: [{ from: "hello", to: "core", kind: "uses", via: "greeter" }],
      warnings: [
        {
          module: "hello",
          message: "a long operator warning that has to wrap on a small phone screen",
        },
      ],
    },
    schema: [{ set: "hello", built: 1, applied: 1, current: true }],
  };

  for (const scheme of ["light", "dark"] as const) {
    test(`lays out at phone width, 44 px targets, axe clean (${scheme})`, async ({
      page,
    }, info) => {
      only("default");
      await page.emulateMedia({ colorScheme: scheme });
      await page.route("**/api/accounts/session", (r) => r.fulfill({ json: person }));
      await mockMe(page);
      await page.route("**/api/admin/modules", (r) => r.fulfill({ json: body }));
      await page.goto("/admin/modules");
      await expect(page.getByRole("heading", { level: 1, name: "Modules" })).toBeVisible();
      await expect(page.getByText("Up to date (version 1)")).toBeVisible();
      if (info.project.name === "phone") {
        await page.setViewportSize({ width: 320, height: 568 });
        await noHorizontalScroll(page);
        await touchTargets(page);
      }
      await axe(page);
    });
  }
});

// ---- accounts (identity.md "UI"): the server is mocked at the network, as for /admin/modules ----

const ME = "0b6f1c3e-0000-4000-8000-000000000002";
const OTHER = "0b6f1c3e-0000-4000-8000-000000000003";
const me = {
  signed_in: true,
  person_id: "7d2c2f0e-0000-4000-8000-000000000001",
  account_id: ME,
  playa_name: "Dusty",
  superuser: false,
  password_change_required: false,
  expires_at: "2099-01-01T00:00:00Z",
  signup_open: true,
  permissions: ["accounts.read", "accounts.manage"],
};
const account = {
  id: OTHER,
  person_id: "7d2c2f0e-0000-4000-8000-000000000009",
  email: "pat@example.test",
  superuser: false,
  password_change_required: false,
  password_changed_at: "2026-10-01T10:00:00Z",
  created_at: "2026-10-01T10:00:00Z",
  updated_at: "2026-10-01T10:00:00Z",
};

/** The session endpoint, signed out until `signIn()` is called. Returns the bodies it saw. */
async function mockIdentity(page: Page, signedInAs: Record<string, unknown> = me, playa = "Dusty") {
  await mockMe(page, playa);
  let signedIn = false;
  const seen: Record<string, unknown[]> = {};
  const record = (path: string, body: unknown) => (seen[path] ??= []).push(body);
  await page.route("**/api/accounts/session", (r) =>
    r.fulfill({
      json: signedIn
        ? signedInAs
        : { signed_in: false, signup_open: true, superuser: false, permissions: [] },
    }),
  );
  for (const path of ["sign-in", "sign-up"]) {
    await page.route(`**/api/accounts/${path}`, (r) => {
      record(path, r.request().postDataJSON());
      signedIn = true;
      return r.fulfill({ status: path === "sign-up" ? 201 : 204 });
    });
  }
  await page.route("**/api/accounts/password", (r) => {
    record("password", r.request().postDataJSON());
    return r.fulfill({ status: 204 });
  });
  await page.route("**/api/accounts/sessions/end-others", (r) => {
    record("end-others", null);
    return r.fulfill({ status: 204 });
  });
  return { seen };
}

test.describe("accounts: sign up, sign in and change a password", () => {
  test("sign-up, with touch alone, lands signed in on the phone", async ({ page }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    const { seen } = await mockIdentity(page, { ...me, playa_name: "" }, "");
    await page.goto("/accounts/sign-up");
    await expect(page.getByRole("heading", { level: 1, name: "Create an account" })).toBeVisible();
    await page.getByLabel(/E-mail address/).fill("new@example.test");
    await page.getByLabel(/^Password/).fill("a long enough password");
    if (isPhone) {
      await noHorizontalScroll(page);
      await touchTargets(page);
    }
    await axe(page);
    await press(page.getByRole("button", { name: "Create account" }), isPhone);
    // no playa name yet: the frame sends the person to the profile prompt, once
    await expect(page).toHaveURL(/\/people\/me\?first=1$/);
    expect(seen["sign-up"]).toEqual([
      { email: "new@example.test", password: "a long enough password" },
    ]);
    // the header shows the neutral label until there is a name
    await openMenu(page, isPhone);
    await expect(
      page.locator("header").getByText("Account", { exact: true }).first(),
    ).toBeVisible();
  });

  test("sign-up errors land on their inputs (taken), accessibly", async ({ page }, info) => {
    only("default");
    await mockIdentity(page);
    await page.route("**/api/accounts/sign-up", (r) =>
      r.fulfill({
        status: 409,
        contentType: "application/problem+json",
        body: JSON.stringify({ status: 409, code: "conflict", ref: "AB12-CD34" }),
      }),
    );
    await page.goto("/accounts/sign-up");
    await page.getByLabel(/E-mail address/).fill("pat@example.test");
    await page.getByLabel(/^Password/).fill("a long enough password");
    await press(
      page.getByRole("button", { name: "Create account" }),
      info.project.name === "phone",
    );
    await expect(page.getByRole("alert")).toContainText("already has an account");
    await expect(page.getByRole("alert")).not.toContainText("AB12-CD34");
    await expect(page.getByLabel(/E-mail address/)).toHaveAttribute("aria-invalid", "true");
    await axe(page);
  });

  test("sign-in follows returnTo and works with the show-password button", async ({
    page,
  }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    const { seen } = await mockIdentity(page);
    await page.goto("/sign-in?returnTo=%2Faccounts%2Fpassword");
    await expect(page).toHaveURL(/\/accounts\/sign-in\?returnTo=%2Faccounts%2Fpassword$/);
    await page.getByLabel(/E-mail address/).fill("pat@example.test");
    await page.getByLabel(/^Password/).fill("correct horse");
    await press(page.getByRole("button", { name: "Show password" }), isPhone);
    await expect(page.getByLabel(/^Password/)).toHaveAttribute("type", "text");
    await press(page.getByRole("button", { name: "Sign in" }), isPhone);
    await expect(page).toHaveURL(/\/accounts\/password$/);
    expect(seen["sign-in"]).toEqual([{ email: "pat@example.test", password: "correct horse" }]);
  });

  test("change password and sign out everywhere else, at the phone viewport", async ({
    page,
  }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    const { seen } = await mockIdentity(page);
    await page.route("**/api/accounts/session", (r) => r.fulfill({ json: me }));
    await page.goto("/accounts/password");
    await expect(page.getByRole("heading", { level: 1, name: "Change password" })).toBeVisible();
    await page.getByLabel(/^Current password/).fill("old password 11");
    await page.getByLabel(/^New password/).fill("new password 22");
    if (isPhone) {
      await noHorizontalScroll(page);
      await touchTargets(page);
    }
    await axe(page);
    await press(page.getByRole("button", { name: "Change password" }), isPhone);
    await expect(page.getByText("Password changed")).toBeVisible();
    expect(seen["password"]).toEqual([
      { current_password: "old password 11", new_password: "new password 22" },
    ]);

    await press(page.getByRole("button", { name: "Sign out everywhere else" }), isPhone);
    const dialog = page.getByRole("dialog", { name: "Sign out everywhere else?" });
    await expect(dialog).toBeVisible();
    if (isPhone) await touchTargets(page);
    await axe(page);
    await press(dialog.getByRole("button", { name: "Sign out other sessions" }), isPhone);
    await expect(page.getByText("Signed out everywhere else")).toBeVisible();
    expect(seen["end-others"]).toHaveLength(1);
  });

  test("a restricted session is held on the password screen from any route", async ({
    page,
  }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    await page.route("**/api/accounts/session", (r) =>
      r.fulfill({ json: { ...me, password_change_required: true } }),
    );
    await page.goto("/hello");
    await expect(page).toHaveURL(/\/accounts\/password\?returnTo=%2Fhello$/);
    await expect(
      page.getByText("You must choose a new password before you continue."),
    ).toBeVisible();
    if (isPhone) {
      await noHorizontalScroll(page);
      await touchTargets(page);
    }
    await axe(page);
  });
});

test.describe("accounts: the admin screens", () => {
  for (const scheme of ["light", "dark"] as const) {
    test(`list and detail lay out at phone width, 44 px targets, axe clean (${scheme})`, async ({
      page,
    }, info) => {
      only("default");
      const isPhone = info.project.name === "phone";
      await page.emulateMedia({ colorScheme: scheme });
      await page.route("**/api/accounts/session", (r) => r.fulfill({ json: me }));
      await mockMe(page);
      await page.route("**/api/accounts/?**", (r) =>
        r.fulfill({
          json: {
            accounts: [
              account,
              {
                ...account,
                id: "0b6f1c3e-0000-4000-8000-000000000005",
                email: "a-rather-long-address-for-narrow-screens@example.test",
                disabled_at: "2026-10-11T10:00:00Z",
              },
            ],
            next: "cursor-1",
          },
        }),
      );
      await page.route("**/api/accounts/", (r) => r.fulfill({ json: { accounts: [account] } }));
      await page.route(`**/api/accounts/${OTHER}`, (r) => r.fulfill({ json: account }));
      await page.route(`**/api/accounts/${OTHER}/temporary-password`, (r) =>
        r.fulfill({ json: { password: "abcd2345efgh6789jkmn2345" } }),
      );
      await page.goto("/accounts/admin");
      await expect(page.getByRole("heading", { level: 1, name: "Accounts" })).toBeVisible();
      await expect(page.getByRole("link", { name: "pat@example.test" })).toBeVisible();
      if (isPhone) {
        await page.setViewportSize({ width: 320, height: 568 });
        await noHorizontalScroll(page);
        await touchTargets(page);
      }
      await axe(page);

      await press(page.getByRole("link", { name: "pat@example.test" }), isPhone);
      await expect(page).toHaveURL(new RegExp(`/accounts/admin/${OTHER}$`));
      await expect(page.getByRole("button", { name: "Disable account" })).toBeVisible();
      if (isPhone) {
        await noHorizontalScroll(page);
        await touchTargets(page);
      }
      await axe(page);

      await press(page.getByRole("button", { name: "Set a temporary password" }), isPhone);
      const confirm = page.getByRole("dialog", { name: "Set a temporary password?" });
      await press(confirm.getByRole("button", { name: "Set temporary password" }), isPhone);
      const shown = page.getByRole("dialog", { name: "Temporary password" });
      await expect(shown.locator("code")).toHaveText("abcd2345efgh6789jkmn2345");
      if (isPhone) {
        await noHorizontalScroll(page);
        await touchTargets(page);
      }
      await axe(page);
    });
  }

  test("403 on the list shows the forbidden state", async ({ page }) => {
    only("default");
    await page.route("**/api/accounts/session", (r) =>
      r.fulfill({ json: { ...me, permissions: [] } }),
    );
    await mockMe(page);
    await page.route("**/api/accounts/?**", (r) =>
      r.fulfill({
        status: 403,
        contentType: "application/problem+json",
        body: JSON.stringify({ status: 403, code: "permission" }),
      }),
    );
    await page.route("**/api/accounts/", (r) =>
      r.fulfill({
        status: 403,
        contentType: "application/problem+json",
        body: JSON.stringify({ status: 403, code: "permission" }),
      }),
    );
    await page.goto("/accounts/admin");
    await expect(page.getByRole("alert")).toContainText(
      "You don't have permission to see accounts.",
    );
    await axe(page);
  });
});

// ---- one real flow, nothing mocked: the server has no seeded accounts, so it signs up first ----

test.describe("a real account (no mocks)", () => {
  test.use({ stubPublicSettings: false }); // the real server answers
  test("sign up, set the playa name, see it in the profile, greeting 200, change password, sign out, sign in, taken", async ({
    page,
  }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    const suffix = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 8)}`;
    const email = `e2e-${suffix}@example.test`;
    const first = `first ${suffix} zebra quartz`;
    const second = `second ${suffix} walrus ember`;
    const playa = `Dusty ${suffix}`;

    // sign up: the frame sends a person with no playa name to the prompt, once
    await page.goto("/accounts/sign-up");
    await page.getByLabel(/E-mail address/).fill(email);
    await page.getByLabel(/^Password/).fill(first);
    await press(page.getByRole("button", { name: "Create account" }), isPhone);
    await expect(page).toHaveURL(/\/people\/me\?first=1$/);
    await expect(page.getByText(/Tell us your playa name/)).toBeVisible();
    await expect(
      page.locator("header span.font-semibold", { hasText: /^Account$/ }),
    ).toBeAttached();

    // set the playa name: the profile shows it and so does the header
    await page.getByLabel(/^Playa name/).fill(playa);
    if (isPhone) {
      await noHorizontalScroll(page);
      await touchTargets(page);
    }
    await axe(page);
    await press(page.getByRole("button", { name: "Save", exact: true }), isPhone);
    await expect(page.getByText("Saved")).toBeVisible();
    await expect(page).toHaveURL(/\/people\/me$/);
    await expect(page.getByLabel(/^Playa name/)).toHaveValue(playa);
    await expect(page.locator("header span.font-semibold", { hasText: playa })).toBeAttached();

    // a reload does not ask again, and the profile still shows the name
    await page.reload();
    await expect(page).toHaveURL(/\/people\/me$/);
    await expect(page.getByLabel(/^Playa name/)).toHaveValue(playa);

    // hello's signed-in part: the participant role holds hello.greetings.read, so 200
    const greeting = page.waitForResponse(
      (r) => new URL(r.url()).pathname === "/api/hello/greeting" && r.request().method() === "GET",
    );
    await page.goto("/hello");
    expect((await greeting).status()).toBe(200);
    await expect(page.getByRole("button", { name: "Say hello" })).toBeVisible();
    await expect(page.getByText("You have greeted never.")).toBeVisible();

    // change the password
    await page.goto("/accounts/password");
    await page.getByLabel(/^Current password/).fill(first);
    await page.getByLabel(/^New password/).fill(second);
    await press(page.getByRole("button", { name: "Change password" }), isPhone);
    await expect(page.getByText("Password changed")).toBeVisible();

    // sign out (on a phone the button is in the menu)
    await openMenu(page, isPhone);
    await press(page.getByRole("button", { name: "Sign out", exact: true }), isPhone);
    await expect(page.getByRole("link", { name: "Sign in" }).first()).toBeVisible();
    await page.waitForURL(/\/$/); // sign-out replaces the page; let it finish before navigating

    // one wrong password, then the new one
    await page.goto("/accounts/sign-in");
    await page.getByLabel(/E-mail address/).fill(email);
    await page.getByLabel(/^Password/).fill(first);
    await press(page.getByRole("button", { name: "Sign in", exact: true }), isPhone);
    await expect(page.getByRole("alert")).toContainText("isn't right");
    await page.getByLabel(/^Password/).fill(second);
    await press(page.getByRole("button", { name: "Sign in", exact: true }), isPhone);
    await expect(page).toHaveURL(/\/$/);
    await expect(page.locator("header span.font-semibold", { hasText: playa })).toBeAttached();

    // the same address again: signed out first, then the taken sentence on the e-mail input
    await openMenu(page, isPhone);
    const reloaded = page.waitForEvent("load"); // sign-out replaces the page
    await press(page.getByRole("button", { name: "Sign out", exact: true }), isPhone);
    await reloaded;
    await page.goto("/accounts/sign-up");
    await page.getByLabel(/E-mail address/).fill(email);
    await page.getByLabel(/^Password/).fill(`third ${suffix} otter lantern`);
    await press(page.getByRole("button", { name: "Create account" }), isPhone);
    await expect(page.getByRole("alert")).toContainText("already has an account");
    await expect(page.getByRole("alert")).not.toContainText(/[0-9A-F]{4}-[0-9A-F]{4}/);
    await expect(page.getByLabel(/E-mail address/)).toHaveAttribute("aria-invalid", "true");
    await axe(page);
  });
});

test.describe("read-only copy: accounts", () => {
  test.use({ stubPublicSettings: false }); // the real server answers
  test("sign-up is disabled and says why", async ({ page }) => {
    only("readonly");
    await page.goto("/accounts/sign-up");
    const button = page.getByRole("button", { name: "Create account" });
    await expect(button).toBeDisabled();
    await expect(
      page.getByText("This copy is read-only, so this can't be saved right now."),
    ).toBeVisible();
  });
});

// ---- access (authz.md "UI"): roles, grants, rules; the server is mocked at the network ----

const R_ADMIN = "0b6f1c3e-0000-4000-8000-0000000000a2";
const R_CREW = "0b6f1c3e-0000-4000-8000-0000000000a4";
const PAT = "7d2c2f0e-0000-4000-8000-000000000009";
const ADMIN_PERMS = [
  "access.roles.read",
  "access.roles.manage",
  "access.rules.manage",
  "access.grants.read",
  "access.grants.manage",
];
const adminSession = (permissions: string[]) => ({
  ...me,
  person_id: "7d2c2f0e-0000-4000-8000-000000000001",
  permissions,
});
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
    await page.route("**/api/people/me/requests", (r) => r.fulfill({ json: { requests: [] } }));
    await page.route("**/api/people/me", (r) => {
      if (r.request().method() === "PUT") {
        saved = (r.request().postDataJSON() as { playa_name: string }).playa_name;
      }
      return r.fulfill({ json: profileOf(saved) });
    });
    await page.goto("/");
    await expect(page).toHaveURL(/\/people\/me\?first=1$/);
    await expect(page.getByText(/Tell us your playa name/)).toBeVisible();
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
      await page.route("**/api/people/me/requests", (r) => r.fulfill({ json: { requests: [] } }));
      await page.route("**/api/people/me/export", (r) =>
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
    await page.route("**/api/people/me/requests", (r) => r.fulfill({ json: { requests: [] } }));
    await page.route("**/api/people/me/export", (r) => r.fulfill({ json: { format: 1 } }));
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
    await page.route("**/api/people/me/requests", (r) => {
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
    await page.route(`**/api/people/me/requests/${REQ}/withdraw`, (r) => {
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

  test("deletion requests: fulfil with a confirmation and read the report", async ({
    page,
  }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    await page.route("**/api/accounts/session", (r) => r.fulfill({ json: holder }));
    await mockMe(page);
    await page.route("**/api/people/requests?**", (r) =>
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
    await page.route(`**/api/people/requests/${REQ}/fulfil`, (r) =>
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

// ---- audit (audit.md "UI"): the server is mocked at the network ----

test.describe("audit: the log screen", () => {
  for (const scheme of ["light", "dark"] as const) {
    test(`filters, entries and export lay out at phone width, 44 px targets, axe clean (${scheme})`, async ({
      page,
    }, info) => {
      only("default");
      const isPhone = info.project.name === "phone";
      await page.emulateMedia({ colorScheme: scheme });
      await page.route("**/api/accounts/session", (r) =>
        r.fulfill({ json: { ...me, permissions: ["audit.search", "audit.export"] } }),
      );
      await mockMe(page);
      await page.route("**/api/people/names?**", (r) =>
        r.fulfill({ json: { names: { [PAT]: "Pat Dust" } } }),
      );
      await page.route(/\/api\/audit\/entries(\?.*)?$/, (r) =>
        r.fulfill({
          json: {
            records: [
              {
                v: 1,
                id: "0b6f1c3e-0000-4000-8000-0000000000f1",
                at: "2026-10-05T10:00:00Z",
                instance: "i",
                action: "people.person.read",
                outcome: "succeeded",
                actor: { kind: "person", person: PAT, titles: ["Access administrator"] },
                subject: PAT,
                level: "personal",
                fields: ["phone", "legal_name"],
                record: { kind: "people.person", id: PAT },
              },
            ],
          },
        }),
      );
      await page.route(`**/api/audit/people/${PAT}/views`, (r) =>
        r.fulfill({ contentType: "application/x-ndjson", body: '{"id":"a"}\n{"id":"b"}\n' }),
      );
      if (isPhone) await page.setViewportSize({ width: 320, height: 568 });
      await page.goto(`/audit?subject=${PAT}`);
      await expect(page.getByRole("heading", { level: 1, name: "Audit log" })).toBeVisible();
      await expect(page.getByText("people.person.read", { exact: true })).toBeVisible();
      if (isPhone) {
        await noHorizontalScroll(page);
        await touchTargets(page);
      }
      await axe(page);
      const download = page.waitForEvent("download");
      await press(page.getByRole("button", { name: "Export views" }), isPhone);
      expect((await download).suggestedFilename()).toBe(`views-${PAT}.jsonl`);
    });
  }
});

// ---- departments and events (departments-events.md "UI"): the server is mocked at the network,
// statefully, so each flow runs end to end through the real client and screens ----

const D_RANGERS = "5c1a0000-0000-4000-8000-0000000000d1";
const EVENT_ID = "e1000000-0000-4000-8000-000000000001";
const SCHEDULE_ITEM = "e2000000-0000-4000-8000-000000000001";

const problemBody = (status: number, code: string, fields: unknown[] = []) => ({
  status,
  contentType: "application/problem+json",
  body: JSON.stringify({ status, code, fields }),
});

/** An in-memory department behind the department routes: none until it is created. */
async function mockDepartments(page: Page, permissions: string[]) {
  await page.route("**/api/accounts/session", (r) =>
    r.fulfill({ json: adminSession(permissions) }),
  );
  await mockMe(page);
  let dept: Record<string, unknown> | null = null;
  const seen: { create?: unknown; update: unknown[] } = { update: [] };
  const now = "2026-10-01T10:00:00Z";
  await page.route(/\/api\/departments\/?(\?.*)?$/, (r) => {
    if (r.request().method() === "POST") {
      seen.create = r.request().postDataJSON();
      dept = {
        id: D_RANGERS,
        archived: false,
        created_at: now,
        updated_at: now,
        ...(seen.create as object),
      };
      return r.fulfill({ status: 201, json: dept });
    }
    const withArchived = new URL(r.request().url()).searchParams.has("include_archived");
    return r.fulfill({
      json: { departments: dept && (!dept["archived"] || withArchived) ? [dept] : [] },
    });
  });
  await page.route(`**/api/departments/${D_RANGERS}`, (r) => {
    if (!dept) return r.fulfill(problemBody(404, "not_found"));
    if (r.request().method() === "PUT") {
      const body = r.request().postDataJSON() as Record<string, unknown>;
      seen.update.push(body);
      dept = { ...dept, ...body, updated_at: new Date().toISOString() };
    }
    return r.fulfill({ json: dept });
  });
  for (const action of ["archive", "restore"]) {
    await page.route(`**/api/departments/${D_RANGERS}/${action}`, (r) => {
      dept = {
        ...dept,
        archived: action === "archive",
        ...(action === "archive" ? { archived_at: "2026-10-10T10:00:00Z" } : {}),
      };
      return r.fulfill({ json: dept });
    });
  }
  return { seen };
}

test.describe("departments: the M3 flows", () => {
  for (const scheme of ["light", "dark"] as const) {
    test(`a steward creates a department, then archives and restores it, with touch alone (${scheme})`, async ({
      page,
    }, info) => {
      only("default");
      const isPhone = info.project.name === "phone";
      await page.emulateMedia({ colorScheme: scheme });
      const { seen } = await mockDepartments(page, ["departments.manage"]);
      if (isPhone) await page.setViewportSize({ width: 320, height: 568 });
      const check = async () => {
        if (isPhone) {
          await noHorizontalScroll(page);
          await touchTargets(page);
        }
        await axe(page);
      };

      await page.goto("/departments");
      await expect(page.getByRole("heading", { level: 1, name: "Departments" })).toBeVisible();
      await expect(page.getByText(/^No department has been created yet\./)).toBeVisible();
      await check();

      await press(page.getByRole("link", { name: "New department" }), isPhone);
      await expect(page).toHaveURL(/\/departments\/new$/);
      await expect(page.getByLabel("Contact mailbox")).toHaveAccessibleDescription(
        "The department's shared mailbox, never a person's address.",
      );
      await page.getByLabel(/^Name/).fill("Rangers");
      await page.getByLabel("Description").fill("Look after people.");
      await page.getByLabel("Contact mailbox").fill("rangers@example.test");
      await check();
      await press(page.getByRole("button", { name: "Create department" }), isPhone);
      await expect(page).toHaveURL(new RegExp(`/departments/${D_RANGERS}$`));
      await expect(page.getByRole("heading", { level: 1, name: "Rangers" })).toBeVisible();
      expect(seen.create).toEqual({
        name: "Rangers",
        description: "Look after people.",
        contact_email: "rangers@example.test",
      });
      await check();

      await press(page.getByRole("button", { name: "Archive", exact: true }), isPhone);
      const archive = page.getByRole("dialog", { name: "Archive this department?" });
      await expect(archive).toContainText("Its leads keep their roles");
      await check();
      await press(archive.getByRole("button", { name: "Archive department" }), isPhone);
      await expect(page.getByText(/^Archived on /)).toBeVisible();
      await expect(page.getByRole("button", { name: "Rename" })).toHaveCount(0);
      await check();

      await press(page.getByRole("button", { name: "Restore", exact: true }), isPhone);
      await press(
        page
          .getByRole("dialog", { name: "Restore this department?" })
          .getByRole("button", { name: "Restore department" }),
        isPhone,
      );
      await expect(page.getByText(/^Archived on /)).toHaveCount(0);
      await expect(page.getByRole("button", { name: "Archive", exact: true })).toBeVisible();

      // the archived toggle follows the list
      await page.goto("/departments");
      await expect(page.getByRole("link", { name: "Rangers", exact: true })).toBeVisible();
      await check();
    });
  }

  test("a department admin at it edits the description and cannot rename or archive", async ({
    page,
  }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    const { seen } = await mockDepartments(page, [`departments.details.manage@${D_RANGERS}`]);
    // the department exists already
    await page.route(/\/api\/departments\/?(\?.*)?$/, (r) =>
      r.fulfill({
        json: {
          departments: [
            {
              id: D_RANGERS,
              name: "Rangers",
              description: "Look after people.",
              archived: false,
              created_at: "2026-10-01T10:00:00Z",
              updated_at: "2026-10-01T10:00:00Z",
            },
          ],
        },
      }),
    );
    await page.route(`**/api/departments/${D_RANGERS}`, (r) => {
      if (r.request().method() === "PUT") seen.update.push(r.request().postDataJSON());
      return r.fulfill({
        json: {
          id: D_RANGERS,
          name: "Rangers",
          description: "Look after people and dust.",
          archived: false,
          created_at: "2026-10-01T10:00:00Z",
          updated_at: "2026-10-02T10:00:00Z",
        },
      });
    });
    await page.goto(`/departments/${D_RANGERS}`);
    await expect(page.getByRole("heading", { level: 1, name: "Rangers" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Rename" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Archive", exact: true })).toHaveCount(0);
    // no grants permission: the Leadership card shows the mailbox note only
    await expect(page.getByRole("region", { name: "Leadership" })).toContainText(
      "no contact mailbox",
    );
    await press(page.getByRole("button", { name: "Edit details" }), isPhone);
    const dialog = page.getByRole("dialog", { name: "Edit details" });
    await dialog.getByLabel("Description").fill("Look after people and dust.");
    if (isPhone) await touchTargets(page);
    await axe(page);
    await press(dialog.getByRole("button", { name: "Save details" }), isPhone);
    await expect(page.getByText("Saved")).toBeVisible();
    expect(seen.update).toEqual([
      { description: "Look after people and dust.", contact_email: "" },
    ]);
  });
});

/** In-memory kinds, events and one event's schedule behind the event routes. */
async function mockEvents(page: Page) {
  await page.route("**/api/accounts/session", (r) =>
    r.fulfill({ json: adminSession(["events.manage"]) }),
  );
  await mockMe(page);
  const stamp = "2026-10-01T10:00:00Z";
  const kinds: Record<string, unknown>[] = ["burn", "decompression", "other"].map((key) => ({
    key,
    name: key.charAt(0).toUpperCase() + key.slice(1),
    builtin: true,
    created_at: stamp,
    updated_at: stamp,
  }));
  let event: Record<string, unknown> | null = null;
  const schedule: Record<string, unknown>[] = [];
  const seen: Record<string, unknown> = {};
  await page.route("**/api/events/kinds", (r) => {
    if (r.request().method() === "POST") {
      const body = r.request().postDataJSON() as Record<string, unknown>;
      seen["kind"] = body;
      kinds.push({ ...body, builtin: false, created_at: stamp, updated_at: stamp });
      return r.fulfill({ status: 201, json: kinds.at(-1) });
    }
    return r.fulfill({ json: { kinds } });
  });
  await page.route(/\/api\/events\/?(\?.*)?$/, (r) => {
    if (r.request().method() === "POST") {
      const body = r.request().postDataJSON() as Record<string, unknown>;
      seen["event"] = body;
      event = { id: EVENT_ID, closed: false, created_at: stamp, updated_at: stamp, ...body };
      delete event["copy_from"];
      return r.fulfill({ status: 201, json: { event, setup: [] } });
    }
    return r.fulfill({ json: { events: event ? [event] : [] } });
  });
  await page.route(`**/api/events/${EVENT_ID}`, (r) => r.fulfill({ json: { ...event, schedule } }));
  await page.route(`**/api/events/${EVENT_ID}/schedule`, (r) => {
    const body = r.request().postDataJSON() as Record<string, unknown>;
    seen["item"] = body;
    schedule.push({ id: SCHEDULE_ITEM, event: EVENT_ID, position: 0, ...body });
    return r.fulfill({ status: 201, json: schedule.at(-1) });
  });
  for (const action of ["close", "reopen"]) {
    await page.route(`**/api/events/${EVENT_ID}/${action}`, (r) => {
      event = {
        ...event,
        closed: action === "close",
        ...(action === "close" ? { closed_at: "2026-10-10T10:00:00Z" } : {}),
      };
      return r.fulfill({ json: event });
    });
  }
  return { seen };
}

test.describe("events: the M3 flows", () => {
  for (const scheme of ["light", "dark"] as const) {
    test(`an event admin creates a kind and an event with a schedule item, closes and reopens it (${scheme})`, async ({
      page,
    }, info) => {
      only("default");
      const isPhone = info.project.name === "phone";
      await page.emulateMedia({ colorScheme: scheme });
      const { seen } = await mockEvents(page);
      if (isPhone) await page.setViewportSize({ width: 320, height: 568 });
      const check = async () => {
        if (isPhone) {
          await noHorizontalScroll(page);
          await touchTargets(page);
        }
        await axe(page);
      };

      // a kind
      await page.goto("/events/kinds");
      await expect(page.getByRole("heading", { level: 1, name: "Event kinds" })).toBeVisible();
      await check();
      await press(page.getByRole("button", { name: "New kind" }), isPhone);
      const kind = page.getByRole("dialog", { name: "New kind" });
      await kind.getByLabel(/^Key/).fill("workshop");
      await kind.getByLabel(/^Name/).fill("Workshop weekend");
      await check();
      await press(kind.getByRole("button", { name: "Add kind" }), isPhone);
      await expect(page.getByRole("heading", { name: "Workshop weekend" })).toBeVisible();
      expect(seen["kind"]).toEqual({ key: "workshop", name: "Workshop weekend" });

      // an event, its dates read in the chosen zone
      await page.goto("/events");
      await expect(page.getByText("No event has been created yet.")).toBeVisible();
      await press(page.getByRole("link", { name: "New event" }), isPhone);
      await expect(page).toHaveURL(/\/events\/new$/);
      await page.getByLabel(/^Name/).fill("Midwinter Gathering");
      await page.getByLabel(/^Kind/).selectOption("workshop");
      await page.getByLabel(/^Time zone/).selectOption("Pacific/Auckland");
      await page.getByLabel(/^Starts/).fill("2027-07-01T12:00");
      await page.getByLabel(/^Ends/).fill("2027-07-03T12:00");
      await check();
      await press(page.getByRole("button", { name: "Create event" }), isPhone);
      await expect(page).toHaveURL(new RegExp(`/events/${EVENT_ID}$`));
      await expect(
        page.getByRole("heading", { level: 1, name: "Midwinter Gathering" }),
      ).toBeVisible();
      expect(seen["event"]).toEqual({
        name: "Midwinter Gathering",
        kind: "workshop",
        dates: { start: "2027-07-01T00:00:00.000Z", end: "2027-07-03T00:00:00.000Z" },
        zone: "Pacific/Auckland",
      });
      await expect(page.getByText("No schedule items yet.")).toBeVisible();
      await check();

      // a schedule item, read in the event's zone
      await press(page.getByRole("button", { name: "Add schedule item" }), isPhone);
      const item = page.getByRole("dialog", { name: "Add schedule item" });
      await item.getByLabel(/^Label/).fill("Gate opens");
      await item.getByLabel("Key").fill("gate-opens");
      await item.getByLabel(/^Starts/).fill("2027-07-01T15:00");
      await check();
      await press(item.getByRole("button", { name: "Save item" }), isPhone);
      await expect(page.getByRole("heading", { level: 3, name: "Gate opens" })).toBeVisible();
      expect(seen["item"]).toEqual({
        label: "Gate opens",
        key: "gate-opens",
        starts_at: "2027-07-01T03:00:00.000Z",
      });
      await check();

      // close, then reopen
      await press(page.getByRole("button", { name: "Close event" }), isPhone);
      const close = page.getByRole("dialog", { name: "Close this event?" });
      await check();
      await press(close.getByRole("button", { name: "Close event" }), isPhone);
      await expect(page.getByText(/was closed on/)).toBeVisible();
      await expect(page.getByRole("button", { name: "Add schedule item" })).toHaveCount(0);
      await check();
      await press(page.getByRole("button", { name: "Reopen", exact: true }), isPhone);
      await press(
        page
          .getByRole("dialog", { name: "Reopen this event?" })
          .getByRole("button", { name: "Reopen event" }),
        isPhone,
      );
      await expect(page.getByText(/was closed on/)).toHaveCount(0);
      await expect(page.getByRole("button", { name: "Add schedule item" })).toBeVisible();
    });
  }

  test("the new-event form shows a server refusal on its inputs", async ({ page }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    await mockEvents(page);
    await page.route(/\/api\/events\/?(\?.*)?$/, (r) =>
      r.request().method() === "POST"
        ? r.fulfill(problemBody(400, "invalid", [{ field: "dates.end", code: "range" }]))
        : r.fulfill({ json: { events: [] } }),
    );
    await page.goto("/events/new");
    await page.getByLabel(/^Name/).fill("Midwinter Gathering");
    await page.getByLabel(/^Starts/).fill("2027-07-03T12:00");
    await page.getByLabel(/^Ends/).fill("2027-07-01T12:00");
    await press(page.getByRole("button", { name: "Create event" }), isPhone);
    await expect(page.getByRole("alert")).toContainText("The end must be after the start");
    await expect(page.getByLabel(/^Ends/)).toHaveAttribute("aria-invalid", "true");
    await axe(page);
  });
});

test.describe("read-only copy: departments and events", () => {
  test("both lists work and their write buttons are disabled with the reason", async ({ page }) => {
    only("readonly");
    await page.route("**/api/accounts/session", (r) =>
      r.fulfill({ json: adminSession(["departments.manage", "events.manage"]) }),
    );
    await mockMe(page);
    const readOnly = { "X-Read-Only": "true" };
    await page.route(/\/api\/departments\/?(\?.*)?$/, (r) =>
      r.fulfill({ headers: readOnly, json: { departments: [] } }),
    );
    await page.route(/\/api\/events\/?(\?.*)?$/, (r) =>
      r.fulfill({ headers: readOnly, json: { events: [] } }),
    );
    await page.route("**/api/events/kinds", (r) =>
      r.fulfill({ headers: readOnly, json: { kinds: [] } }),
    );
    await page.goto("/departments");
    await expect(page.getByRole("heading", { level: 1, name: "Departments" })).toBeVisible();
    await expect(page.getByRole("button", { name: "New department" })).toBeDisabled();
    await page.goto("/events");
    await expect(page.getByRole("heading", { level: 1, name: "Events" })).toBeVisible();
    await expect(page.getByRole("button", { name: "New event" })).toBeDisabled();
  });
});

// ---- settings (M4): the regional's values, terminology and an area's form, mocked at the network ----

/** An in-memory settings document behind the settings routes. The public document follows what
 *  was saved, as the real one does. */
async function mockSettings(page: Page, permissions: string[]) {
  await page.route("**/api/accounts/session", (r) =>
    r.fulfill({ json: adminSession(permissions) }),
  );
  await mockMe(page);
  const state = {
    name: "BMS",
    color: "",
    departments: "Departments",
    contrastError: true,
    saved: [] as unknown[],
    termSaves: [] as unknown[],
  };
  const text = (key: string, over: Record<string, unknown> = {}) => ({
    key,
    kind: "text",
    default: "",
    value: null,
    secret_set: false,
    stale: false,
    ...over,
  });
  await mockPublicSettings(page, () =>
    publicSettings({
      name: state.name,
      terms: {
        en: {
          term_department: state.departments.toLowerCase(),
          term_department_plural: state.departments.toLowerCase(),
          term_department_title: state.departments,
          term_department_plural_title: state.departments,
        },
      },
    }),
  );
  await page.route(/\/api\/settings\/?$/, (r) =>
    r.fulfill({
      json: {
        revision: 3,
        areas: [
          {
            name: "regional",
            core: true,
            settings: [
              text("regional.name", {
                default: "BMS",
                value: state.name === "BMS" ? null : state.name,
              }),
              text("regional.contact_email"),
              text("regional.public_url"),
              text("regional.language", { kind: "enum", default: "en", options: ["en", "es"] }),
              text("regional.timezone"),
              text("regional.color_primary", { value: state.color || null }),
              text("regional.color_primary_dark"),
            ],
          },
          {
            name: "accounts",
            core: true,
            settings: [
              text("accounts.session_lifetime", { kind: "duration", default: "720h" }),
              text("accounts.password_min_length", { kind: "int", default: "12" }),
            ],
          },
          {
            name: "hello",
            core: false,
            settings: [
              text("hello.greeting", { default: "Hello" }),
              text("hello.token", { kind: "secret", secret_set: true }),
            ],
          },
        ],
      },
    }),
  );
  await page.route("**/api/settings/values", (r) => {
    const body = r.request().postDataJSON() as { values: Record<string, string | null> };
    state.saved.push(body);
    const color = body.values["regional.color_primary"];
    if (color && state.contrastError) {
      state.contrastError = false;
      return r.fulfill({
        status: 400,
        contentType: "application/problem+json",
        body: JSON.stringify({
          status: 400,
          code: "invalid",
          fields: [
            {
              field: "regional.color_primary",
              code: "contrast",
              reason: "too faint",
              params: { ratio: "1.6", min: "4.5" },
            },
          ],
        }),
      });
    }
    if (body.values["regional.name"]) state.name = body.values["regional.name"];
    if (color) state.color = color;
    return r.fulfill({ json: { revision: 4 } });
  });
  const form = (singular: string, plural: string, st: string, pt: string) => ({
    singular,
    plural,
    singular_title: st,
    plural_title: pt,
  });
  await page.route("**/api/settings/terms", (r) =>
    r.fulfill({
      json: {
        languages: ["en"],
        terms: {
          en: {
            department: {
              default: form("department", "departments", "Department", "Departments"),
              override:
                state.departments === "Departments" ? {} : { plural_title: state.departments },
            },
            event: {
              default: form("event", "events", "Event", "Events"),
              override: {},
            },
          },
        },
      },
    }),
  );
  await page.route("**/api/settings/terms/en", (r) => {
    const body = r.request().postDataJSON() as {
      terms: Record<string, { plural_title?: string }>;
    };
    state.termSaves.push(body);
    state.departments = body.terms["department"]?.plural_title ?? state.departments;
    return r.fulfill({ json: { revision: 5 } });
  });
  return { state };
}

test.describe("settings: the M4 screens", () => {
  for (const scheme of ["light", "dark"] as const) {
    test(`an admin changes the name and colour, fixes a contrast error, and renames Departments, with touch alone (${scheme})`, async ({
      page,
    }, info) => {
      only("default");
      const isPhone = info.project.name === "phone";
      await page.emulateMedia({ colorScheme: scheme });
      const { state } = await mockSettings(page, ["settings.read", "settings.manage"]);
      if (isPhone) await page.setViewportSize({ width: 320, height: 568 });
      const check = async () => {
        if (isPhone) {
          await noHorizontalScroll(page);
          await touchTargets(page);
        }
        await axe(page);
      };

      await page.goto("/settings");
      await expect(page.getByRole("heading", { level: 1, name: "Settings" })).toBeVisible();
      await expect(page.getByRole("link", { name: "BMS", exact: true }).first()).toBeVisible();
      await expect(page.getByLabel("Contact mailbox")).toHaveAttribute("type", "email");
      await expect(page.getByLabel("Web address")).toHaveAttribute("type", "url");
      await check();

      // a colour that is too faint: said live, refused by the server, the edits are kept
      await page.getByLabel(/^Name/).fill("Playa Hall");
      await page.getByLabel("Brand colour", { exact: true }).fill("#cccccc");
      await expect(page.getByText(/too low \(at least 4\.5 to 1 is needed\)/)).toBeVisible();
      await press(page.getByRole("button", { name: "Save" }), isPhone);
      const alert = page.getByRole("alert");
      await expect(alert).toContainText("Brand colour");
      await expect(alert).toContainText("1.6 to 1");
      await expect(page.getByLabel("Brand colour", { exact: true })).toHaveAttribute(
        "aria-invalid",
        "true",
      );
      await expect(page.getByLabel(/^Name/)).toHaveValue("Playa Hall");
      await check();

      await page.getByLabel("Brand colour", { exact: true }).fill("#0b5cad");
      await press(page.getByRole("button", { name: "Save" }), isPhone);
      await expect(page.getByText("Saved")).toBeVisible();
      // the header follows without a reload
      await expect(
        page.getByRole("banner").getByRole("link", { name: "Playa Hall" }),
      ).toBeVisible();
      expect(state.saved.at(-1)).toEqual({
        values: { "regional.name": "Playa Hall", "regional.color_primary": "#0b5cad" },
      });

      await press(page.getByRole("link", { name: "Terminology" }), isPhone);
      await expect(page).toHaveURL(/\/settings\/terms$/);
      const group = page.getByRole("group", { name: "Department" });
      await group.getByLabel("Several, as a title").fill("Teams");
      await check();
      await press(page.getByRole("button", { name: "Save English" }), isPhone);
      // toasts stack: the name save's may still be up, so wait for the terms save itself,
      // then look at the newest toast
      await expect.poll(() => state.termSaves.length).toBe(1);
      await expect(page.getByText("Saved").last()).toBeVisible();
      expect(state.termSaves).toEqual([{ terms: { department: { plural_title: "Teams" } } }]);
      const nav = await openMenu(page, isPhone);
      await expect(nav.getByRole("link", { name: "Teams" })).toBeVisible();
    });
  }

  test("an area form shows a duration as a number and unit and never shows a secret", async ({
    page,
  }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    const { state } = await mockSettings(page, ["settings.read", "settings.manage"]);
    if (isPhone) await page.setViewportSize({ width: 320, height: 568 });

    await page.goto("/settings");
    await press(page.getByRole("link", { name: "Accounts" }), isPhone);
    await expect(page).toHaveURL(/\/settings\/accounts$/);
    await expect(page.getByLabel("How long people stay signed in", { exact: true })).toHaveValue(
      "30",
    );
    await expect(page.getByLabel("Unit for How long people stay signed in")).toHaveValue("d");
    await page.getByLabel("How long people stay signed in", { exact: true }).fill("14");
    await press(page.getByRole("button", { name: "Save" }), isPhone);
    await expect(page.getByText("Saved")).toBeVisible();
    expect(state.saved.at(-1)).toEqual({ values: { "accounts.session_lifetime": "336h" } });

    await page.goto("/settings/hello");
    await expect(page.getByText("A value is set. It can't be shown.")).toBeVisible();
    await expect(page.locator('input[type="password"]')).toHaveCount(0);
    await press(page.getByRole("button", { name: "Replace: hello.token" }), isPhone);
    await expect(page.getByLabel("New value for hello.token")).toHaveAttribute("type", "password");
    if (isPhone) {
      await noHorizontalScroll(page);
      await touchTargets(page);
    }
    await axe(page);
  });

  test("a participant cannot open /settings and has no menu entry for it", async ({
    page,
  }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    await mockSettings(page, []);
    await page.goto("/settings");
    await expect(page.getByText("You don't have access to this screen.")).toBeVisible();
    await expect(page.getByLabel(/^Name/)).toHaveCount(0);
    if (isPhone) await page.getByRole("button", { name: "Menu" }).tap();
    await expect(page.getByRole("link", { name: "Settings", exact: true })).toHaveCount(0);
    await axe(page);
  });

  test("the departments page title follows a term rename", async ({ page }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    await mockSettings(page, ["settings.read", "settings.manage"]);
    await page.route(/\/api\/departments\/?(\?.*)?$/, (r) =>
      r.fulfill({ json: { departments: [] } }),
    );
    await page.goto("/settings/terms");
    await page
      .getByRole("group", { name: "Department" })
      .getByLabel("Several, as a title")
      .fill("Teams");
    await press(page.getByRole("button", { name: "Save English" }), isPhone);
    await expect(page.getByText("Saved")).toBeVisible();
    await page.goto("/departments");
    await expect(page.getByRole("heading", { level: 1, name: "Teams" })).toBeVisible();
    await expect(page).toHaveTitle(/^Teams · /);
  });

  test("a person who can manage but not read gets an explanation and no form", async ({ page }) => {
    only("default");
    await mockSettings(page, ["settings.manage"]);
    await page.goto("/settings");
    await expect(page.getByText(/change settings but not read them/)).toBeVisible();
    await expect(page.getByLabel(/^Name/)).toHaveCount(0);
    await axe(page);
  });
});

test.describe("read-only copy: settings", () => {
  test("Save is disabled with the reason and the form keeps its edits", async ({ page }) => {
    only("readonly");
    await mockSettings(page, ["settings.read", "settings.manage"]);
    const readOnly = { "X-Read-Only": "true" };
    await page.route(/\/api\/settings\/?$/, (r) =>
      r.fulfill({
        headers: readOnly,
        json: {
          revision: 1,
          areas: [
            {
              name: "regional",
              core: true,
              settings: [
                {
                  key: "regional.name",
                  kind: "text",
                  default: "BMS",
                  value: null,
                  secret_set: false,
                  stale: false,
                },
              ],
            },
          ],
        },
      }),
    );
    await page.goto("/settings");
    await page.getByLabel(/^Name/).fill("Playa Hall");
    await expect(page.getByRole("button", { name: "Save" })).toBeDisabled();
    await expect(
      page.getByText("This copy is read-only, so this can't be saved right now."),
    ).toBeVisible();
    await expect(page.getByLabel(/^Name/)).toHaveValue("Playa Hall");
  });
});

test.describe("accounts: the forgotten-password flow", () => {
  const SECRET = "e2e-reset-token-value";

  test("ask for a link, then choose a new password from it, with touch alone; the token leaves the address", async ({
    page,
  }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    if (isPhone) await page.setViewportSize({ width: 320, height: 568 });
    await signedOut(page);
    const requests: unknown[] = [];
    const completes: unknown[] = [];
    await page.route("**/api/accounts/password-reset", (r) => {
      requests.push(r.request().postDataJSON());
      return r.fulfill({ status: 202 });
    });
    await page.route("**/api/accounts/password-reset/complete", (r) => {
      completes.push(r.request().postDataJSON());
      return r.fulfill({ status: 204 });
    });
    const check = async () => {
      if (isPhone) {
        await noHorizontalScroll(page);
        await touchTargets(page);
      }
      await axe(page);
    };

    await page.goto("/accounts/sign-in");
    await press(page.getByRole("link", { name: "Forgot your password?" }), isPhone);
    await expect(page).toHaveURL(/\/accounts\/password-reset$/);
    await expect(page.getByLabel(/E-mail address/)).toHaveAttribute("type", "email");
    await check();
    await page.getByLabel(/E-mail address/).fill("someone@example.test");
    await press(page.getByRole("button", { name: "Send the link" }), isPhone);
    await expect(
      page.getByText("If an account exists for that address, an e-mail is on its way."),
    ).toBeVisible();
    expect(requests).toEqual([{ email: "someone@example.test" }]);
    await check();

    await page.goto(`/accounts/password-reset?token=${SECRET}`);
    await expect(
      page.getByRole("heading", { level: 1, name: "Choose a new password" }),
    ).toBeVisible();
    await expect(page).toHaveURL(/\/accounts\/password-reset$/);
    expect(page.url()).not.toContain(SECRET);
    expect(
      await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage })),
    ).not.toContain(SECRET);
    await check();
    await page.getByLabel(/^New password(?! again)/).fill("a long new passphrase");
    await page.getByLabel(/New password again/).fill("a long new passphrase");
    await press(page.getByRole("button", { name: "Change password" }), isPhone);
    await expect(page).toHaveURL(/\/accounts\/sign-in$/);
    await expect(page.getByText("Password changed. Sign in with your new password.")).toBeVisible();
    expect(completes).toEqual([{ token: SECRET, new_password: "a long new passphrase" }]);
    await check();
  });

  test("501 says e-mail isn't set up; an expired link has its own sentence and a way to ask again", async ({
    page,
  }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    await signedOut(page);
    await page.route("**/api/accounts/password-reset", (r) =>
      r.fulfill({
        status: 501,
        contentType: "application/problem+json",
        body: JSON.stringify({ status: 501, code: "unsupported" }),
      }),
    );
    await page.route("**/api/accounts/password-reset/complete", (r) =>
      r.fulfill({
        status: 400,
        contentType: "application/problem+json",
        body: JSON.stringify({
          status: 400,
          code: "invalid",
          fields: [{ field: "token", code: "expired", reason: "expired" }],
        }),
      }),
    );
    await page.goto("/accounts/password-reset");
    await page.getByLabel(/E-mail address/).fill("someone@example.test");
    await press(page.getByRole("button", { name: "Send the link" }), isPhone);
    await expect(page.getByRole("alert")).toContainText(
      "Password reset by e-mail isn't set up here. Ask an administrator for a temporary password.",
    );
    await axe(page);

    await page.goto(`/accounts/password-reset?token=${SECRET}`);
    await page.getByLabel(/^New password(?! again)/).fill("a long new passphrase");
    await page.getByLabel(/New password again/).fill("a long new passphrase");
    await press(page.getByRole("button", { name: "Change password" }), isPhone);
    await expect(page.getByRole("alert")).toContainText("This link has expired.");
    await press(page.getByRole("link", { name: "Ask for a new link" }), isPhone);
    await expect(page.getByRole("button", { name: "Send the link" })).toBeVisible();
    await axe(page);
  });
});

test.describe("notify: the e-mail admin screens", () => {
  const NAME = "identity.password-reset";
  const MSG = "9a1f0000-0000-4000-8000-000000000001";
  const viewOf = (subject: string, body: string, overridden: boolean) => ({
    name: NAME,
    language: "en",
    overridden,
    vars: ["lifetime"],
    default: { subject: "Reset your password", body: "Open {{.Link}} within {{.Vars.lifetime}}." },
    current: { subject, body },
  });

  test("edit a template, preview it, save it, reset it and send a test, with touch alone", async ({
    page,
  }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    if (isPhone) await page.setViewportSize({ width: 320, height: 568 });
    await page.route("**/api/accounts/session", (r) =>
      r.fulfill({ json: adminSession(["notify.templates.manage", "notify.outbox.manage"]) }),
    );
    await mockMe(page);
    const saved: unknown[] = [];
    let overridden = false;
    let text = {
      subject: "Reset your password",
      body: "Open {{.Link}} within {{.Vars.lifetime}}.",
    };
    await page.route("**/api/notify/templates", (r) =>
      r.fulfill({
        json: [
          {
            name: NAME,
            available: true,
            vars: ["lifetime"],
            languages: [{ language: "en", overridden }],
          },
        ],
      }),
    );
    await page.route(`**/api/notify/templates/${NAME}/en`, (r) => {
      const method = r.request().method();
      if (method === "PUT") {
        const body = r.request().postDataJSON() as typeof text;
        saved.push(body);
        text = body;
        overridden = true;
      }
      if (method === "DELETE") {
        overridden = false;
        text = {
          subject: "Reset your password",
          body: "Open {{.Link}} within {{.Vars.lifetime}}.",
        };
        return r.fulfill({ status: 204 });
      }
      return r.fulfill({ json: viewOf(text.subject, text.body, overridden) });
    });
    await page.route(`**/api/notify/templates/${NAME}/en/preview`, (r) =>
      r.fulfill({ json: { subject: "Rendered subject", body: "Rendered body" } }),
    );
    await page.route("**/api/notify/test", (r) => r.fulfill({ status: 202, json: { id: MSG } }));
    const check = async () => {
      if (isPhone) {
        await noHorizontalScroll(page);
        await touchTargets(page);
      }
      await axe(page);
    };

    await page.goto("/notify/templates");
    await expect(page.getByRole("heading", { level: 1, name: "E-mail templates" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Password-reset link" })).toBeVisible();
    await check();
    await press(page.getByRole("button", { name: "Send me a test" }), isPhone);
    await expect(page.getByText(MSG)).toBeVisible();
    await press(page.getByRole("link", { name: "en: shipped text" }), isPhone);
    await expect(page).toHaveURL(/\/notify\/templates\/identity\.password-reset\/en$/);

    await page.getByLabel(/^Subject/).fill("Choose a new password");
    await press(page.getByRole("button", { name: "Preview" }), isPhone);
    await expect(page.getByText("Rendered subject")).toBeVisible();
    await check();
    await press(page.getByRole("button", { name: "Save" }), isPhone);
    await expect(page.getByText("Saved", { exact: true })).toBeVisible();
    expect(saved).toEqual([
      { subject: "Choose a new password", body: "Open {{.Link}} within {{.Vars.lifetime}}." },
    ]);

    await press(page.getByRole("button", { name: "Reset to default" }), isPhone);
    const dialog = page.getByRole("dialog");
    await press(dialog.getByRole("button", { name: "Reset to default" }), isPhone);
    await expect(page.getByLabel(/^Subject/)).toHaveValue("Reset your password");
    await expect(page.getByRole("button", { name: "Reset to default" })).toHaveCount(0);
    await check();
  });

  test("the outbox: counts, retry, cancel and the connection check, with no address on the page", async ({
    page,
  }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    if (isPhone) await page.setViewportSize({ width: 320, height: 568 });
    await page.route("**/api/accounts/session", (r) =>
      r.fulfill({ json: adminSession(["notify.outbox.manage"]) }),
    );
    await mockMe(page);
    const calls: string[] = [];
    await page.route("**/api/notify/outbox", (r) =>
      r.fulfill({
        json: {
          configured: true,
          counts: { queued: 1, sending: 0, sent: 12, failed: 1 },
          recent: [
            {
              id: MSG,
              owner: "identity",
              template: "password-reset",
              language: "en",
              status: "failed",
              attempts: 3,
              created_at: "2026-10-19T10:00:00Z",
              expires_at: "2099-01-01T00:00:00Z",
              last_error: "connect",
            },
          ],
        },
      }),
    );
    await page.route("**/api/people/names?**", (r) => r.fulfill({ json: { names: {} } }));
    await page.route(`**/api/notify/outbox/${MSG}/retry`, (r) => {
      calls.push("retry");
      return r.fulfill({
        status: 409,
        contentType: "application/problem+json",
        body: JSON.stringify({
          status: 409,
          code: "conflict",
          fields: [{ field: "status", code: "expired", reason: "expired" }],
        }),
      });
    });
    await page.route(`**/api/notify/outbox/${MSG}/cancel`, (r) => {
      calls.push("cancel");
      return r.fulfill({ status: 204 });
    });
    await page.route("**/api/notify/outbox/check", (r) => r.fulfill({ status: 204 }));
    const check = async () => {
      if (isPhone) {
        await noHorizontalScroll(page);
        await touchTargets(page);
      }
      await axe(page);
    };

    await page.goto("/notify/outbox");
    await expect(page.getByRole("heading", { level: 1, name: "E-mail outbox" })).toBeVisible();
    await expect(page.getByText(MSG)).toBeVisible();
    await expect(page.getByText("Couldn't reach the mail server", { exact: true })).toBeVisible();
    await expect(page.getByText("Password-reset link", { exact: true })).toBeVisible();
    await expect(page.getByText("An e-mail address (no account)")).toBeVisible();
    expect(await page.locator("main").innerText()).not.toContain("@");
    await check();
    await press(page.getByRole("button", { name: "Check connection" }), isPhone);
    await expect(page.getByText(/mail server answered/)).toBeVisible();
    await press(page.getByRole("button", { name: `Retry message ${MSG}` }), isPhone);
    await expect(page.getByRole("alert")).toContainText("This message has expired");
    await press(page.getByRole("button", { name: `Cancel message ${MSG}` }), isPhone);
    await press(page.getByRole("dialog").getByRole("button", { name: "Cancel message" }), isPhone);
    await expect(page.getByText("Message canceled")).toBeVisible();
    expect(calls).toEqual(["retry", "cancel"]);
    await check();
  });

  test("a participant has no entry and gets the forbidden state", async ({ page }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    await page.route("**/api/accounts/session", (r) => r.fulfill({ json: adminSession([]) }));
    await mockMe(page);
    await page.goto("/notify/outbox");
    await expect(page.getByText("You don't have access to this screen.")).toBeVisible();
    if (isPhone) await page.getByRole("button", { name: "Menu" }).tap();
    await expect(page.getByRole("link", { name: "E-mail outbox" })).toHaveCount(0);
    await axe(page);
  });
});

test.describe("jobs: the background work screen", () => {
  const FAILED = "5b1f0000-0000-4000-8000-000000000001";
  const record = {
    id: FAILED,
    name: "audit.purge",
    status: "failed",
    attempts: 3,
    run_at: "2026-10-19T10:00:00Z",
    last_error: "gave_up",
  };

  test("status, run now and retry, with touch alone", async ({ page }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    if (isPhone) await page.setViewportSize({ width: 320, height: 568 });
    await page.route("**/api/accounts/session", (r) =>
      r.fulfill({ json: adminSession(["jobs.read", "jobs.manage"]) }),
    );
    await mockMe(page);
    const calls: string[] = [];
    await page.route("**/api/jobs/", (r) =>
      r.fulfill({
        json: {
          counts: { failed: 1 },
          read_only: false,
          registered: [
            {
              name: "jobs.purge",
              owner: "jobs",
              every_seconds: 3600,
              timeout_seconds: 600,
              next_due: "2026-10-20T12:00:00Z",
            },
          ],
          recent: [record],
        },
      }),
    );
    await page.route("**/api/jobs/registered/jobs.purge/run", (r) => {
      calls.push("run");
      return r.fulfill({ status: 202, json: { id: FAILED } });
    });
    await page.route(`**/api/jobs/queue/${FAILED}/retry`, (r) => {
      calls.push("retry");
      return r.fulfill({ status: 204 });
    });
    await page.goto("/jobs");
    await expect(page.getByRole("heading", { level: 1, name: "Background work" })).toBeVisible();
    await expect(page.getByText("Runs every 1 hour")).toBeVisible();
    await expect(page.getByText("The job kept failing and was given up on")).toBeVisible();
    if (isPhone) {
      await noHorizontalScroll(page);
      await touchTargets(page);
    }
    await axe(page);
    await press(page.getByRole("button", { name: "Run now: jobs.purge" }), isPhone);
    await press(page.getByRole("button", { name: /^Retry audit\.purge/ }), isPhone);
    await expect.poll(() => calls).toEqual(["run", "retry"]);
  });

  test("a participant gets the forbidden state and no entry", async ({ page }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    await page.route("**/api/accounts/session", (r) => r.fulfill({ json: adminSession([]) }));
    await mockMe(page);
    await page.goto("/jobs");
    await expect(page.getByText("You don't have access to this screen.")).toBeVisible();
    if (isPhone) await page.getByRole("button", { name: "Menu" }).tap();
    await expect(page.getByRole("link", { name: "Background work" })).toHaveCount(0);
    await axe(page);
  });
});
