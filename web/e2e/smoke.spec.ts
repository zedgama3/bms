import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";
import type { Locator, Page } from "@playwright/test";

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

/** A tap on a phone, a click on the desktop: every flow must work with touch alone. */
async function press(target: Locator, isPhone: boolean) {
  if (isPhone) await target.tap();
  else await target.click();
}

test.describe("the frame", () => {
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
  playa_name: "",
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
async function mockIdentity(page: Page, signedInAs: Record<string, unknown> = me) {
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
    const { seen } = await mockIdentity(page);
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
    await expect(page).toHaveURL(/\/$/);
    expect(seen["sign-up"]).toEqual([
      { email: "new@example.test", password: "a long enough password" },
    ]);
    // no playa name yet: the header shows the neutral label
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
  test("sign up, forbidden hello, change password, sign out, sign in, taken", async ({
    page,
  }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    const suffix = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 8)}`;
    const email = `e2e-${suffix}@example.test`;
    const first = `first ${suffix} zebra quartz`;
    const second = `second ${suffix} walrus ember`;

    // sign up: lands on / with the neutral label (no playa name yet)
    await page.goto("/accounts/sign-up");
    await page.getByLabel(/E-mail address/).fill(email);
    await page.getByLabel(/^Password/).fill(first);
    await press(page.getByRole("button", { name: "Create account" }), isPhone);
    await expect(page).toHaveURL(/\/$/);
    await expect(
      page.locator("header span.font-semibold", { hasText: /^Account$/ }),
    ).toBeAttached();

    // hello's signed-in part is forbidden until roles exist (M2); the person stays signed in
    await page.goto("/hello");
    await expect(page.getByRole("alert")).toContainText("You don't have permission");
    await expect(page.locator("header span.font-semibold")).toBeAttached();

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

    // one wrong password, then the new one
    await page.goto("/accounts/sign-in");
    await page.getByLabel(/E-mail address/).fill(email);
    await page.getByLabel(/^Password/).fill(first);
    await press(page.getByRole("button", { name: "Sign in", exact: true }), isPhone);
    await expect(page.getByRole("alert")).toContainText("isn't right");
    await page.getByLabel(/^Password/).fill(second);
    await press(page.getByRole("button", { name: "Sign in", exact: true }), isPhone);
    await expect(page).toHaveURL(/\/$/);
    await expect(page.locator("header span.font-semibold")).toBeAttached();

    // the same address again: signed out first, then the taken sentence on the e-mail input
    await openMenu(page, isPhone);
    await press(page.getByRole("button", { name: "Sign out", exact: true }), isPhone);
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
