import AxeBuilder from "@axe-core/playwright";
import { expect, test as base } from "@playwright/test";
import type { Locator, Page, Route } from "@playwright/test";

// The volunteers module's main flows on a phone and on the desktop (module-volunteers.md "UI",
// web-shell.md "Test plan"). The server is mocked at the network, as for the accounts flows, so
// the run needs no seeded data. Invented names only.

const mode = process.env["BMS_E2E_MODE"] ?? "default";
const only = (m: string) => test.skip(mode !== m, `needs BMS_E2E_MODE=${m}`);

const test = base.extend<object>({
  page: async ({ page }, provide) => {
    await page.route("**/api/settings/public", (r) =>
      r.fulfill({
        json: {
          name: "BMS",
          logo: "",
          contact_email: "",
          language: "en",
          languages: ["en"],
          public_url: "",
          theme: false,
          timezone: "",
          terms: {},
        },
      }),
    );
    await page.route("**/api/settings/theme.css**", (r) =>
      r.fulfill({ contentType: "text/css", body: "" }),
    );
    await provide(page);
  },
});

const ME = "7d2c2f0e-0000-4000-8000-000000000001";
const OTHER = "7d2c2f0e-0000-4000-8000-000000000009";
const DEPT = "5c1a0000-0000-4000-8000-0000000000d1";
const EVENT = "5c1a0000-0000-4000-8000-0000000000e1";
const POS = "5c1a0000-0000-4000-8000-0000000000b1";
const QUAL = "5c1a0000-0000-4000-8000-0000000000a1";
const SHIFT = "5c1a0000-0000-4000-8000-0000000000c1";
const SIGNUP = "5c1a0000-0000-4000-8000-0000000000f1";
const RECORD = "5c1a0000-0000-4000-8000-0000000000f2";

const LEAD = [
  "shifts.signup",
  "shifts.manage",
  "positions.manage",
  "members.manage",
  "qualifications.verify",
  "rosters.read",
].map((p) => (p === "shifts.signup" ? `volunteers.${p}` : `volunteers.${p}@${DEPT}`));

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

/** A tap on a phone, a click on the desktop: every flow must work with touch alone. */
/** A list answer as the real server sends it: an empty list is `{"items":[]}` (V1.8 (g)). */
const listOf = (items: unknown[]) => ({ items });

/** A record as the server echoes it: fields at their zero value (empty text, false, empty list)
 *  are omitted. */
function wire(o: object): Record<string, unknown> {
  return Object.fromEntries(
    Object.entries(o).filter(
      ([, v]) => v !== "" && v !== false && !(Array.isArray(v) && v.length === 0),
    ),
  );
}

async function press(target: Locator, isPhone: boolean) {
  if (isPhone) await target.tap();
  else await target.click();
}

async function phoneChecks(page: Page, isPhone: boolean) {
  if (!isPhone) return;
  await page.setViewportSize({ width: 320, height: 568 });
  await noHorizontalScroll(page);
  await touchTargets(page);
}

const problem = (status: number, code: string) => ({
  status,
  contentType: "application/problem+json",
  body: JSON.stringify({ status, code }),
});

/** Mocks the frame's reads for a signed-in person holding the given permissions. */
async function signIn(page: Page, permissions: string[]) {
  await page.route("**/api/accounts/session", (r) =>
    r.fulfill({
      json: {
        signed_in: true,
        person_id: ME,
        account_id: "0b6f1c3e-0000-4000-8000-000000000002",
        playa_name: "Dusty",
        superuser: false,
        password_change_required: false,
        expires_at: "2099-01-01T00:00:00Z",
        signup_open: false,
        permissions,
      },
    }),
  );
  await page.route("**/api/people/me", (r) => r.fulfill(problem(404, "not_found")));
  await page.route("**/api/modules", (r) =>
    r.fulfill({
      json: {
        modules: [
          { name: "hello", enabled: true },
          { name: "volunteers", enabled: true },
        ],
      },
    }),
  );
  await page.route("**/api/departments/", (r) =>
    r.fulfill({
      json: { departments: [{ id: DEPT, name: "Gate", description: "", archived: false }] },
    }),
  );
  await page.route("**/api/events/", (r) =>
    r.fulfill({
      json: {
        events: [
          {
            id: EVENT,
            name: "Midsummer",
            zone: "America/Denver",
            closed: false,
            kind: "main",
            dates: { start: "2099-06-28T00:00:00Z", end: "2099-07-05T00:00:00Z" },
          },
        ],
      },
    }),
  );
  await page.route("**/api/people/names**", (r) =>
    r.fulfill({ json: { names: { [OTHER]: "Pat Dust", [ME]: "Dusty" } } }),
  );
}

/** A tiny in-test volunteers server: `handler` answers "METHOD /api/volunteers/..." keys. */
type Handler = (route: Route, body: unknown) => Promise<void> | void;
async function volunteers(page: Page, handlers: Record<string, Handler>) {
  await page.route("**/api/volunteers/**", async (route) => {
    const req = route.request();
    const key = `${req.method()} ${new URL(req.url()).pathname}`;
    const h = handlers[key];
    if (!h) return route.fulfill(problem(404, "not_found"));
    let body: unknown = null;
    if (req.postData()) body = req.postDataJSON();
    return h(route, body);
  });
}

const V = "/api/volunteers";
const shiftOf = (over: Record<string, unknown> = {}) => ({
  id: SHIFT,
  department: DEPT,
  department_name: "Gate",
  position: POS,
  position_name: "Greeter",
  label: "Morning dirt",
  starts: "2099-07-01T15:00:00Z",
  ends: "2099-07-01T19:00:00Z",
  zone: "America/Denver",
  slots: 4,
  location: "Ranger HQ",
  ...over,
});
const catalogue = (route: Route) =>
  route.fulfill({ json: { items: [{ id: QUAL, name: "First aid", validity_days: 365 }] } });

test.describe("volunteers: a lead creates a position and a shift", () => {
  test("positions tab, then the shift form, ends on the new shift", async ({ page }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    await signIn(page, LEAD);
    const positions: unknown[] = [];
    const shifts: Record<string, unknown>[] = [];
    const posted: { position?: unknown; shift?: unknown } = {};
    await volunteers(page, {
      [`GET ${V}/qualifications`]: catalogue,
      [`GET ${V}/departments/${DEPT}/positions`]: (r) => r.fulfill({ json: listOf(positions) }),
      [`POST ${V}/departments/${DEPT}/positions`]: (r, body) => {
        posted.position = body;
        positions.push(wire({ id: POS, department: DEPT, ...(body as object), requirements: [] }));
        return r.fulfill({ status: 201, json: positions[0] });
      },
      [`GET ${V}/shifts`]: (r) => r.fulfill({ json: listOf(shifts) }),
      [`POST ${V}/departments/${DEPT}/shifts`]: (r, body) => {
        posted.shift = body;
        shifts.push(wire(shiftOf()));
        return r.fulfill({ status: 201, json: shifts[0] });
      },
      [`GET ${V}/shifts/${SHIFT}`]: (r) => r.fulfill({ json: shiftOf({ booked: 0 }) }),
      [`GET ${V}/shifts/${SHIFT}/roster`]: (r) => r.fulfill({ json: { shift: shiftOf() } }),
    });

    await page.goto(`/volunteers/departments/${DEPT}?tab=positions`);
    await expect(page.getByRole("heading", { level: 1, name: "Gate" })).toBeVisible();
    await expect(page.getByText("No positions yet. Create the first one.")).toBeVisible();
    await phoneChecks(page, isPhone);
    await axe(page);

    await press(page.getByRole("button", { name: "New position" }), isPhone);
    const dialog = page.getByRole("dialog", { name: "New position" });
    await dialog.getByLabel(/^Name/).fill("Greeter");
    if (isPhone) await touchTargets(page);
    await axe(page);
    await press(dialog.getByRole("button", { name: "Save position" }), isPhone);
    await expect(page.getByRole("heading", { level: 3, name: "Greeter" })).toBeVisible();
    expect(posted.position).toMatchObject({
      name: "Greeter",
      access: "open",
      hide_shift_mates: false,
    });

    await press(page.getByRole("link", { name: "Shifts", exact: true }).last(), isPhone);
    await press(page.getByRole("link", { name: "New shift" }), isPhone);
    await expect(page).toHaveURL(new RegExp(`/departments/${DEPT}/shifts/new$`));
    await expect(page.getByRole("heading", { level: 1, name: "New shift" })).toBeVisible();
    await page.getByLabel(/^Position/).selectOption(POS);
    await page.getByLabel("Event").selectOption(EVENT);
    await page.getByLabel(/^Name of the shift/).fill("Morning dirt");
    await page.getByLabel(/^Starts/).fill("2099-07-01T09:00");
    await page.getByLabel(/^Ends/).fill("2099-07-01T13:00");
    await page.getByLabel(/^Places/).fill("4");
    await page.getByLabel("Where").fill("Ranger HQ");
    await phoneChecks(page, isPhone);
    await axe(page);
    await press(page.getByRole("button", { name: "Create shift" }), isPhone);
    await expect(page).toHaveURL(new RegExp(`/volunteers/shifts/${SHIFT}$`));
    await expect(page.getByRole("heading", { level: 1, name: "Morning dirt" })).toBeVisible();
    expect(posted.shift).toMatchObject({
      position: POS,
      event: EVENT,
      starts: "2099-07-01T15:00:00.000Z",
      ends: "2099-07-01T19:00:00.000Z",
      slots: 4,
    });
    await axe(page);
  });
});

test.describe("volunteers: a volunteer signs up and cancels", () => {
  for (const scheme of ["light", "dark"] as const) {
    test(`sign up from the list, see who else is on the shift, cancel (${scheme})`, async ({
      page,
    }, info) => {
      only("default");
      const isPhone = info.project.name === "phone";
      await page.emulateMedia({ colorScheme: scheme });
      await signIn(page, ["volunteers.shifts.signup"]);
      let booked = false;
      const summary = () =>
        booked
          ? shiftOf({ booked: 3, mine: SIGNUP, mates: { names: ["Moth", "Ember"], unlisted: 1 } })
          : shiftOf({ booked: 2, eligibility: { eligible: true }, mates: { names: ["Moth"] } });
      await volunteers(page, {
        [`GET ${V}/qualifications`]: catalogue,
        [`GET ${V}/departments/${DEPT}/positions`]: (r) =>
          r.fulfill({
            json: { items: [{ id: POS, department: DEPT, name: "Greeter", access: "open" }] },
          }),
        [`GET ${V}/shifts`]: (r) => r.fulfill({ json: { items: [summary()] } }),
        [`GET ${V}/shifts/${SHIFT}`]: (r) => r.fulfill({ json: summary() }),
        [`POST ${V}/shifts/${SHIFT}/signups`]: (r) => {
          booked = true;
          return r.fulfill({ status: 201, json: { id: SIGNUP, shift: SHIFT } });
        },
        [`DELETE ${V}/signups/${SIGNUP}`]: (r) => {
          booked = false;
          return r.fulfill({ status: 204 });
        },
      });

      await page.goto("/volunteers");
      await expect(page.getByRole("heading", { level: 1, name: "Shifts" })).toBeVisible();
      const card = page.getByRole("listitem").filter({ hasText: "Morning dirt" });
      await expect(card.getByText("2 of 4 booked")).toBeVisible();
      await phoneChecks(page, isPhone);
      await axe(page);
      // the primary action is within the first viewport (the one phoneChecks set, not a guess);
      // the filters are folded away on a phone so the first card is on screen
      if (isPhone) {
        await expect(page.getByRole("button", { name: "Filters" })).toHaveAttribute(
          "aria-expanded",
          "false",
        );
        const box = await card.getByRole("button", { name: /^Sign up/ }).boundingBox();
        const height = page.viewportSize()?.height ?? 0;
        expect(box && box.y + box.height <= height).toBe(true);
      }
      await press(card.getByRole("button", { name: /^Sign up/ }), isPhone);
      await expect(card.getByText("You're booked")).toBeVisible();

      await press(card.getByRole("link", { name: "Morning dirt" }), isPhone);
      await expect(page).toHaveURL(new RegExp(`/volunteers/shifts/${SHIFT}$`));
      const mates = page.getByRole("region", { name: "Who else is on this shift" });
      await expect(mates.getByText("Ember")).toBeVisible();
      await expect(mates.getByText("and 1 person who is not listed")).toBeVisible();
      await phoneChecks(page, isPhone);
      await axe(page);

      await press(page.getByRole("button", { name: "Cancel my booking" }), isPhone);
      const dialog = page.getByRole("dialog", { name: "Cancel your booking?" });
      await axe(page);
      await press(dialog.getByRole("button", { name: "Cancel my booking" }), isPhone);
      await expect(page.getByRole("button", { name: /^Sign up/ })).toBeVisible();
      await expect(page.getByRole("button", { name: "Cancel my booking" })).toHaveCount(0);
    });
  }

  test("a full shift says so and offers no Sign up", async ({ page }, info) => {
    only("default");
    await signIn(page, ["volunteers.shifts.signup"]);
    await volunteers(page, {
      [`GET ${V}/qualifications`]: catalogue,
      [`GET ${V}/departments/${DEPT}/positions`]: (r) => r.fulfill({ json: {} }),
      [`GET ${V}/shifts`]: (r) =>
        r.fulfill({ json: { items: [shiftOf({ booked: 4, eligibility: { reason: "full" } })] } }),
    });
    await page.goto("/volunteers");
    await expect(page.getByText("Full", { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: /^Sign up/ })).toHaveCount(0);
    if (info.project.name === "phone") await phoneChecks(page, true);
    await axe(page);
  });
});

test.describe("volunteers: a lead verifies a qualification", () => {
  test("a certificate that is ready opens from a link the person taps", async ({ page }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    await signIn(page, LEAD);
    await volunteers(page, {
      [`GET ${V}/qualifications`]: catalogue,
      [`GET ${V}/qualification-records/${RECORD}/certificate`]: (r) =>
        r.fulfill({ status: 200, contentType: "application/pdf", body: "%PDF-1.4" }),
      [`GET ${V}/qualification-records`]: (r) =>
        r.fulfill({
          json: listOf([
            {
              id: RECORD,
              qualification: QUAL,
              person: OTHER,
              acquired_on: "2026-01-02",
              certificate: "9b1f0000-0000-4000-8000-0000000000f3",
            },
          ]),
        }),
    });
    await page.goto(`/volunteers/departments/${DEPT}?tab=pending`);
    await press(page.getByRole("button", { name: /^Open\s*Certificate/ }), isPhone);
    const link = page.getByRole("link", { name: /^Certificate: .*open in a new tab$/ });
    await expect(link).toBeVisible();
    await expect(link).toHaveAttribute("href", /^blob:/);
    await expect(link).toHaveAttribute("target", "_blank");
    await expect(link).toBeFocused();
    await phoneChecks(page, isPhone);
    await axe(page);
  });

  test("pending tab: open the file, correct a date, verify", async ({ page }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    await signIn(page, LEAD);
    let pending = [
      {
        id: RECORD,
        qualification: QUAL,
        person: OTHER,
        acquired_on: "2026-01-02",
        certificate: "9b1f0000-0000-4000-8000-0000000000f3",
      },
    ];
    let verifiedWith: unknown = null;
    await volunteers(page, {
      [`GET ${V}/qualifications`]: catalogue,
      [`GET ${V}/qualification-records/${RECORD}/certificate`]: (r) =>
        r.fulfill(problem(404, "not_found")),
      [`GET ${V}/qualification-records`]: (r) => r.fulfill({ json: listOf(pending) }),
      [`POST ${V}/qualification-records/${RECORD}/verify`]: (r, body) => {
        verifiedWith = body;
        pending = [];
        return r.fulfill({ json: { id: RECORD } });
      },
    });

    await page.goto(`/volunteers/departments/${DEPT}?tab=pending`);
    await expect(page.getByRole("heading", { level: 3, name: "First aid" })).toBeVisible();
    await expect(page.getByRole("link", { name: "Pat Dust" })).toBeVisible();
    // the certificate opens through the shell's Attachments row, never a raw link
    await expect(page.getByRole("link", { name: /Open/ })).toHaveCount(0);
    await press(page.getByRole("button", { name: /^Open\s*Certificate/ }), isPhone);
    await expect(page.getByText("This certificate is no longer available.")).toBeVisible();
    await phoneChecks(page, isPhone);
    await axe(page);

    await press(page.getByRole("button", { name: /^Verify/ }), isPhone);
    const dialog = page.getByRole("dialog", { name: "Verify First aid" });
    await dialog.getByLabel(/^Acquired on/).fill("2026-01-03");
    await dialog.getByLabel(/^Expires on/).fill("2027-01-03");
    if (isPhone) await touchTargets(page);
    await axe(page);
    await press(dialog.getByRole("button", { name: "Verify" }), isPhone);
    await expect(page.getByText("No certificates are waiting.")).toBeVisible();
    expect(verifiedWith).toEqual({ acquired_on: "2026-01-03", expires_on: "2027-01-03" });
  });

  test("Never expires clears and disables the end date and sends no_expiry", async ({
    page,
  }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    await signIn(page, LEAD);
    let verifiedWith: unknown = null;
    await volunteers(page, {
      [`GET ${V}/qualifications`]: catalogue,
      [`GET ${V}/qualification-records`]: (r) =>
        r.fulfill({
          json: listOf([
            { id: RECORD, qualification: QUAL, person: OTHER, acquired_on: "2026-01-02" },
          ]),
        }),
      [`POST ${V}/qualification-records/${RECORD}/verify`]: (r, body) => {
        verifiedWith = body;
        return r.fulfill({ json: { id: RECORD } });
      },
    });
    await page.goto(`/volunteers/departments/${DEPT}?tab=pending`);
    await press(page.getByRole("button", { name: /^Verify/ }), isPhone);
    const dialog = page.getByRole("dialog", { name: "Verify First aid" });
    await press(dialog.getByLabel("Never expires"), isPhone);
    await expect(dialog.getByLabel(/^Expires on/)).toBeDisabled();
    if (isPhone) await touchTargets(page);
    await axe(page);
    await press(dialog.getByRole("button", { name: "Verify" }), isPhone);
    await expect.poll(() => verifiedWith).toEqual({ acquired_on: "2026-01-02", no_expiry: true });
  });
});

test.describe("volunteers: my volunteering on a phone", () => {
  test("lays out at 320 px, 44 px targets, axe clean; tabs, removed bookings, Show more, upload", async ({
    page,
  }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    await signIn(page, ["volunteers.shifts.signup"]);
    const booking = (id: string, label: string, over: Record<string, unknown> = {}) => ({
      id,
      status: "booked",
      shift: shiftOf({ id: `${id}-shift`, label }),
      department_name: "Gate",
      position_name: "Greeter",
      ...over,
    });
    const pastAsked: string[] = [];
    await volunteers(page, {
      [`GET ${V}/qualifications`]: catalogue,
      [`GET ${V}/me/schedule`]: (r) => {
        const q = new URL(r.request().url()).searchParams;
        if (q.get("past") === "true") {
          pastAsked.push("past");
          return r.fulfill({ json: listOf([booking("p1", "Old build shift")]) });
        }
        return r.fulfill({
          json:
            q.get("after") === "c1"
              ? listOf([booking("u2", "Second page shift")])
              : {
                  items: [
                    booking(SIGNUP, "Morning dirt"),
                    booking("u3", "Removed shift", { status: "canceled", cancel_reason: "lead" }),
                  ],
                  next: "c1",
                },
        });
      },
      [`GET ${V}/me/departments`]: (r) =>
        r.fulfill({ json: { items: [{ id: "m1", department: DEPT, person: ME }] } }),
      [`GET ${V}/people/${ME}/qualifications`]: (r) =>
        r.fulfill({ json: { items: [], self_upload: true } }),
      [`GET ${V}/me/listing`]: (r) => r.fulfill({ json: { listed: true } }),
    });
    await page.goto("/volunteers/me");
    await expect(page.getByRole("heading", { level: 1, name: "My volunteering" })).toBeVisible();
    await expect(page.getByRole("link", { name: "Morning dirt" })).toBeVisible();
    await expect(page.getByText("Removed by the lead")).toBeVisible();
    await expect(page.getByRole("heading", { level: 3, name: "Removed shift" })).toHaveClass(
      /line-through/,
    );
    await expect(page.getByLabel("Show my playa name to others on my shifts")).toBeChecked();
    await expect(page.getByRole("heading", { name: "Upload a certificate" })).toBeVisible();
    await phoneChecks(page, isPhone);
    await axe(page);

    await press(page.getByRole("button", { name: "Show more" }), isPhone);
    await expect(page.getByRole("link", { name: "Second page shift" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Show more" })).toHaveCount(0);

    await press(page.getByRole("button", { name: "Past", exact: true }), isPhone);
    await expect(page.getByRole("link", { name: "Old build shift" })).toBeVisible();
    expect(pastAsked.length).toBeGreaterThan(0);
    await phoneChecks(page, isPhone);
    await axe(page);
  });
});

test.describe("volunteers: a manager without roster rights assigns from the shift page", () => {
  test("Manage shows Assign (waiver for themself allowed); an unnamed person is explained", async ({
    page,
  }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    await signIn(page, ["volunteers.shifts.signup", `volunteers.shifts.manage@${DEPT}`]);
    let assigned: unknown = null;
    await volunteers(page, {
      [`GET ${V}/qualifications`]: catalogue,
      [`GET ${V}/shifts/${SHIFT}`]: (r) => r.fulfill({ json: shiftOf({ booked: 0 }) }),
      [`POST ${V}/shifts/${SHIFT}/assign`]: (r, body) => {
        assigned = body;
        const person = (body as { person: string }).person;
        return person === OTHER
          ? r.fulfill({
              status: 409,
              contentType: "application/problem+json",
              body: JSON.stringify({
                status: 409,
                code: "conflict",
                fields: [{ field: "person", code: "unnamed", reason: "" }],
              }),
            })
          : r.fulfill({ status: 201, json: { id: SIGNUP, shift: SHIFT, person } });
      },
    });
    await page.route("**/api/people/search?**", (r) =>
      r.fulfill({
        json: {
          matches: [
            { id: ME, playa_name: "Dusty", pronouns: "" },
            { id: OTHER, pronouns: "" },
          ],
        },
      }),
    );
    await page.goto(`/volunteers/shifts/${SHIFT}`);
    await expect(page.getByRole("heading", { level: 2, name: "Manage this shift" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Roster" })).toHaveCount(0);
    await phoneChecks(page, isPhone);
    await page.getByLabel("Playa name").fill("Du");
    await press(page.getByRole("button", { name: "Search", exact: true }), isPhone);
    await press(page.getByRole("button", { name: /No playa name yet/ }), isPhone);
    await press(page.getByRole("button", { name: "Assign", exact: true }), isPhone);
    await expect(page.getByRole("alert")).toContainText(
      "This person has not set a playa name yet.",
    );
    await axe(page);

    await press(page.getByRole("button", { name: /Change|Choose someone else/ }).first(), isPhone);
    await page.getByLabel("Playa name").fill("Du");
    await press(page.getByRole("button", { name: "Search", exact: true }), isPhone);
    await press(page.getByRole("button", { name: /Dusty/ }), isPhone);
    const waive = page.getByLabel("Waive requirements");
    await expect(waive).toBeEnabled();
    await press(waive, isPhone);
    await press(page.getByRole("button", { name: "Assign", exact: true }), isPhone);
    await expect.poll(() => assigned).toEqual({ person: ME, waive: true });
  });
});
