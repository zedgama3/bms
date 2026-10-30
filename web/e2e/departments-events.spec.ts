// End-to-end smoke run, departments and events, and their read-only copy (web-shell.md decision 22, "Test plan").
import { expect } from "@playwright/test";
import type { Page } from "@playwright/test";
import {
  noHorizontalScroll,
  axe,
  touchTargets,
  mockMe,
  press,
  test,
  only,
  adminSession,
} from "./support/helpers";

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
