// End-to-end smoke run, sign up, sign in, password change and reset, the admin screens and one real account flow (web-shell.md decision 22, "Test plan").
import { expect } from "@playwright/test";
import type { Page } from "@playwright/test";
import {
  noHorizontalScroll,
  axe,
  touchTargets,
  openMenu,
  signedOut,
  mockMe,
  press,
  test,
  only,
  me,
} from "./support/helpers";

// ---- accounts (identity.md "UI"): the server is mocked at the network, as for /admin/modules ----

const OTHER = "0b6f1c3e-0000-4000-8000-000000000003";
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
    await expect(page.getByRole("heading", { level: 1, name: "Account" })).toBeVisible();
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
    // playa names are permanent (people P1): once set the field is read-only, with the reason
    await expect(page.getByLabel(/^Playa name/)).toHaveAttribute("readonly", "");
    await expect(page.getByText(/Playa names are permanent/)).toBeVisible();
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

    // a second person tries the same playa name with the case and spacing changed (people P1)
    await page.getByLabel(/E-mail address/).fill(`e2e-b-${suffix}@example.test`);
    await page.getByLabel(/^Password/).fill(`fourth ${suffix} heron cobalt`);
    await press(page.getByRole("button", { name: "Create account" }), isPhone);
    await expect(page).toHaveURL(/\/people\/me\?first=1$/);
    await page.getByLabel(/^Playa name/).fill(`  DUSTY   ${suffix.toUpperCase()} `);
    await press(page.getByRole("button", { name: "Save", exact: true }), isPhone);
    await expect(
      page.locator("p.text-danger", {
        hasText: "Someone here already uses this playa name. Choose another.",
      }),
    ).toBeVisible();
    await expect(page.getByLabel(/^Playa name/)).toHaveAttribute("aria-invalid", "true");
    // the owner's name is never shown to the person who collided with it
    await expect(page.locator("body")).not.toContainText(playa);
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
