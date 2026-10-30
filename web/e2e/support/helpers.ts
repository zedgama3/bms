import AxeBuilder from "@axe-core/playwright";
import { expect, test as base } from "@playwright/test";
import type { Locator, Page } from "@playwright/test";

// Shared helpers, the extended `test` and the mode gate for the end-to-end specs
// (web-shell.md decision 22, "Test plan").

/** The public settings document the shell reads at boot (`GET /api/settings/public`). */
export function publicSettings(over: Record<string, unknown> = {}) {
  return {
    name: "BMS",
    logo: "",
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
export async function mockPublicSettings(page: Page, doc: () => unknown = () => publicSettings()) {
  await page.route("**/api/settings/public", (r) => r.fulfill({ json: doc() }));
  await page.route("**/api/settings/theme.css**", (r) =>
    r.fulfill({ contentType: "text/css", body: "" }),
  );
}

/** Every test mocks the public settings, except the ones that talk to the real server
 *  (`test.use({ stubPublicSettings: false })`), which get the real document and theme. */
export const test = base.extend<{ stubPublicSettings: boolean }>({
  stubPublicSettings: [true, { option: true }],
  page: async ({ page, stubPublicSettings }, provide) => {
    if (stubPublicSettings) await mockPublicSettings(page);
    await provide(page);
  },
});

export const mode = process.env["BMS_E2E_MODE"] ?? "default";
export const only = (m: string) => test.skip(mode !== m, `needs BMS_E2E_MODE=${m}`);

/** Console messages that mean the CSP blocked something. */
export function watchCsp(page: Page): string[] {
  const violations: string[] = [];
  page.on("console", (m) => {
    if (/content security policy|refused to (apply|load|execute|connect)/i.test(m.text()))
      violations.push(m.text());
  });
  page.on("pageerror", (e) => violations.push(`pageerror: ${e.message}`));
  return violations;
}

export async function noHorizontalScroll(page: Page) {
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  expect(overflow, "horizontal overflow in CSS px").toBeLessThanOrEqual(0);
}

export async function axe(page: Page) {
  const results = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"])
    .analyze();
  expect(
    results.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`),
  ).toEqual([]);
}

/** On a phone every control is at least 44 x 44 CSS px. */
export async function touchTargets(page: Page) {
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
export async function openMenu(page: Page, isPhone: boolean): Promise<Locator> {
  if (isPhone) {
    const menu = page.getByRole("button", { name: "Menu" });
    await expect(menu).toHaveAttribute("aria-expanded", "false");
    await menu.tap();
    await expect(menu).toHaveAttribute("aria-expanded", "true");
  }
  return page.getByRole("navigation", { name: "Main" });
}

/** The session answer for a signed-out visitor on a copy where sign-up is open. */
export async function signedOut(page: Page) {
  await page.route("**/api/accounts/session", (r) =>
    r.fulfill({ json: { signed_in: false, signup_open: true, superuser: false, permissions: [] } }),
  );
}

export const NOBODY = {
  edit: false,
  read_contacts: false,
  export: false,
  delete: false,
  link: false,
  view_roles: false,
  view_audit: false,
};
export const OPTIONAL = {
  legal_name: "optional",
  phone: "optional",
  emergency_contacts: "optional",
};

/** The own-profile answer (`GET /api/people/me`) the frame reads once per session. */
export function profileOf(playa: string, over: Record<string, unknown> = {}) {
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
    // the server says so once per session; an unnamed mock is a first read (people P-W1)
    ...(playa === "" ? { first_prompt: true } : {}),
    ...over,
  };
}

/** Mocks the own profile for a mocked session (an unmocked call would reach the real server
 *  with a cookie it never issued and sign the page out). */
export async function mockMe(page: Page, playa = "Dusty", over: Record<string, unknown> = {}) {
  await page.route("**/api/people/me", (r) => r.fulfill({ json: profileOf(playa, over) }));
  await page.route("**/api/people/me/requests", (r) => r.fulfill({ json: { requests: [] } }));
}

/** A tap on a phone, a click on the desktop: every flow must work with touch alone. */
export async function press(target: Locator, isPhone: boolean) {
  if (isPhone) await target.tap();
  else await target.click();
}

// ---- shared by the mocked-session specs ----

/** The account the mocked sessions belong to. */
export const ME = "0b6f1c3e-0000-4000-8000-000000000002";
export const me = {
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

/** The person other people's screens look at. */
export const PAT = "7d2c2f0e-0000-4000-8000-000000000009";

/** A signed-in session holding exactly `permissions`. */
export const adminSession = (permissions: string[]) => ({
  ...me,
  person_id: "7d2c2f0e-0000-4000-8000-000000000001",
  permissions,
});
