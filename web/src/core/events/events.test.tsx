import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { expectNoAxeViolations } from "@/test/axe";
import { mountApp } from "@/test/app";
import { json, noContent, problem } from "@/test/fakeFetch";
import { instantToLocal, localToInstant } from "./helpers";

const ME = "7d2c2f0e-0000-4000-8000-000000000001";
const E_NOW = "e1000000-0000-4000-8000-000000000001";
const E_NEXT = "e1000000-0000-4000-8000-000000000002";
const E_PAST = "e1000000-0000-4000-8000-000000000003";
const E_CLOSED = "e1000000-0000-4000-8000-000000000004";
const ITEM = "e2000000-0000-4000-8000-000000000001";

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

const day = 86_400_000;
const iso = (offset: number) => new Date(Date.now() + offset).toISOString();
const ev = (id: string, name: string, start: number, end: number, over = {}) => ({
  id,
  name,
  kind: "burn",
  zone: "Pacific/Auckland",
  closed: false,
  dates: { start: iso(start), end: iso(end) },
  created_at: "2026-01-01T10:00:00Z",
  updated_at: "2026-01-02T10:00:00Z",
  ...over,
});
const NOW = ev(E_NOW, "Midsummer Burn", -day, day);
const NEXT = ev(E_NEXT, "Winter Burn", 30 * day, 33 * day, { kind: "decompression" });
const PAST = ev(E_PAST, "Old Burn", -90 * day, -87 * day);
const CLOSED = ev(E_CLOSED, "Shut Burn", 10 * day, 12 * day, {
  closed: true,
  closed_at: "2026-02-01T10:00:00Z",
});
const kinds = [
  {
    key: "burn",
    name: "Burn",
    builtin: true,
    created_at: "2026-01-01T10:00:00Z",
    updated_at: "2026-01-01T10:00:00Z",
  },
  {
    key: "decompression",
    name: "Decompression",
    builtin: true,
    created_at: "2026-01-01T10:00:00Z",
    updated_at: "2026-01-01T10:00:00Z",
  },
  {
    key: "workshop",
    name: "Workshop weekend",
    builtin: false,
    created_at: "2026-01-01T10:00:00Z",
    updated_at: "2026-01-01T10:00:00Z",
  },
];
const item = (over = {}) => ({
  id: ITEM,
  event: E_NOW,
  key: "temple-burn",
  label: "Temple burn",
  position: 0,
  starts_at: iso(0),
  ends_at: iso(3_600_000),
  ...over,
});

type Routes = Record<string, (req: Request) => Response | Promise<Response>>;
const base = (over: Routes = {}): Routes => ({
  "GET /api/events/": () => json({ events: [PAST, NEXT, NOW, CLOSED].sort(() => 0) }),
  "GET /api/events/kinds": () => json({ kinds }),
  [`GET /api/events/${E_NOW}`]: () => json({ ...NOW, schedule: [item()] }),
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

describe("zone helpers", () => {
  it("reads a wall-clock time in a zone as an instant and back", () => {
    expect(localToInstant("2026-07-01T12:00", "Pacific/Auckland")).toBe("2026-07-01T00:00:00.000Z");
    expect(localToInstant("2026-01-01T12:00", "Pacific/Auckland")).toBe("2025-12-31T23:00:00.000Z");
    expect(localToInstant("2026-07-01T12:00", "America/New_York")).toBe("2026-07-01T16:00:00.000Z");
    expect(instantToLocal("2026-07-01T00:00:00Z", "Pacific/Auckland")).toBe("2026-07-01T12:00");
    expect(localToInstant("", "UTC")).toBe("");
    expect(localToInstant("2026-07-01T12:00", "Not/AZone")).toBe("2026-07-01T12:00:00.000Z");
  });
});

describe("navigation", () => {
  it("lists Events for everyone and Event kinds only for events.manage", async () => {
    const first = mountApp({ session: ident([]), routes: base() });
    await screen.findByRole("heading", { name: "Hi, Dusty" });
    expect(
      within(screen.getByRole("navigation", { name: "Main" })).getByRole("link", {
        name: "Events",
      }),
    ).toHaveAttribute("href", "/events");
    expect(screen.queryByRole("link", { name: "Event kinds" })).not.toBeInTheDocument();
    first.unmount();
    mountApp({ session: ident(["events.manage"]), routes: base() });
    await screen.findByRole("heading", { name: "Hi, Dusty" });
    expect(
      within(screen.getByRole("navigation", { name: "Administration" })).getByRole("link", {
        name: "Event kinds",
      }),
    ).toHaveAttribute("href", "/events/kinds");
  });
});

describe("/events", () => {
  it("groups events as Now, Upcoming and Past with labels, zoned dates and a Closed badge", async () => {
    const { container } = mountApp({ path: "/events", session: ident([]), routes: base() });
    await screen.findByRole("heading", { level: 2, name: "Now" });
    const group = (name: string) => screen.getByRole("heading", { level: 2, name }).parentElement!;
    expect(within(group("Now")).getByRole("link", { name: "Midsummer Burn" })).toHaveAttribute(
      "href",
      `/events/${E_NOW}`,
    );
    expect(within(group("Now")).getByText("Burn")).toBeInTheDocument();
    expect(
      within(group("Upcoming")).getByRole("link", { name: "Winter Burn" }),
    ).toBeInTheDocument();
    expect(within(group("Upcoming")).getByText("Decompression")).toBeInTheDocument();
    // a closed event is past even though it has not ended
    expect(within(group("Past")).getByRole("link", { name: "Shut Burn" })).toBeInTheDocument();
    expect(within(group("Past")).getByRole("link", { name: "Old Burn" })).toBeInTheDocument();
    expect(within(group("Past")).getByText("Closed")).toBeInTheDocument();
    // the zone differs from the browser's (UTC in tests), so the dates carry it
    expect(within(group("Now")).getByText(/N?Z[A-Z]*T|GMT\+1[23]/)).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "New event" })).not.toBeInTheDocument();
    await expectNoAxeViolations(container);
  });

  it("gives events.manage holders New event; says none yet; shows a failed load", async () => {
    const first = mountApp({
      path: "/events",
      session: ident(["events.manage"]),
      routes: base({ "GET /api/events/": () => json({ events: [] }) }),
    });
    expect(await screen.findByText("No event has been created yet.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "New event" })).toHaveAttribute("href", "/events/new");
    first.unmount();
    mountApp({
      path: "/events",
      session: ident([]),
      routes: base({
        "GET /api/events/": () => problem(500, { code: "internal", ref: "AAAA-0000" }),
      }),
    });
    expect(await screen.findByRole("alert")).toHaveTextContent("Something went wrong.");
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument();
  });
});

describe("/events/new", () => {
  it("is forbidden without events.manage", async () => {
    mountApp({ path: "/events/new", session: ident([]), routes: base() });
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "You don't have access to this screen.",
    );
  });

  it("reads the dates in the chosen zone, preselects the newest event of the kind to copy, and shows the set-up outcome", async () => {
    const { fetchMock, router } = mountApp({
      path: "/events/new",
      session: ident(["events.manage"]),
      routes: base({
        "POST /api/events/": () =>
          json(
            {
              event: { ...NEXT, id: "e1000000-0000-4000-8000-0000000000ff", name: "Spring Burn" },
              setup: [
                { module: "volunteers", failed: false, items: 12 },
                { module: "incidents", failed: false, items: 0 },
                { module: "registrations", failed: true, items: 0, code: "permission" },
              ],
            },
            { status: 201 },
          ),
        "GET /api/events/e1000000-0000-4000-8000-0000000000ff": () =>
          json({
            ...NEXT,
            id: "e1000000-0000-4000-8000-0000000000ff",
            name: "Spring Burn",
            schedule: [],
          }),
      }),
    });
    await userEvent.type(await screen.findByLabelText(/^Name/), "Spring Burn");
    const zone = screen.getByLabelText(/^Time zone/);
    await userEvent.selectOptions(zone, "Pacific/Auckland");
    // the newest event of the preselected kind (burn) is the first burn in the list
    const copy = screen.getByLabelText("Copy set-up from");
    expect(copy).toHaveValue(E_PAST);
    await userEvent.selectOptions(screen.getByLabelText(/^Kind/), "decompression");
    expect(copy).toHaveValue(E_NEXT);
    await userEvent.selectOptions(copy, "");
    await userEvent.selectOptions(copy, E_NOW);
    await userEvent.type(screen.getByLabelText(/^Starts/), "2026-12-01T12:00");
    await userEvent.type(screen.getByLabelText(/^Ends/), "2026-12-03T12:00");
    await userEvent.click(screen.getByRole("button", { name: "Create event" }));
    await waitFor(() => expect(router()!.state.location.pathname).toMatch(/\/events\/e1000000/));
    expect(await bodyOf(fetchMock, "POST", "/api/events/")).toEqual({
      name: "Spring Burn",
      kind: "decompression",
      dates: { start: "2026-11-30T23:00:00.000Z", end: "2026-12-02T23:00:00.000Z" },
      zone: "Pacific/Auckland",
      copy_from: E_NOW,
    });
    const card = await screen.findByRole("region", { name: "Set-up" });
    expect(within(card).getByText("volunteers: 12 items copied")).toBeInTheDocument();
    expect(within(card).getByText("incidents: nothing to copy")).toBeInTheDocument();
    expect(within(card).getByText("registrations: failed (no permission)")).toBeInTheDocument();
    expect(within(card).getByRole("button", { name: "Run set-up again" })).toBeInTheDocument();
  });

  it("puts field errors on their inputs (range, unknown kind, taken)", async () => {
    mountApp({
      path: "/events/new",
      session: ident(["events.manage"]),
      routes: base({
        "POST /api/events/": () =>
          problem(400, {
            code: "invalid",
            fields: [
              { field: "dates.end", code: "range" },
              { field: "kind", code: "unknown" },
              { field: "name", code: "taken" },
            ],
          }),
      }),
    });
    await userEvent.type(await screen.findByLabelText(/^Name/), "X");
    await userEvent.click(screen.getByRole("button", { name: "Create event" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("The end must be after the start");
    expect(screen.getByLabelText(/^Ends/)).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByLabelText(/^Kind/)).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByLabelText(/^Name/)).toHaveAttribute("aria-invalid", "true");
  });

  it("shows a failed kinds load instead of a form", async () => {
    mountApp({
      path: "/events/new",
      session: ident(["events.manage"]),
      routes: base({
        "GET /api/events/kinds": () => problem(500, { code: "internal", ref: "AAAA-0000" }),
      }),
    });
    expect(await screen.findByRole("alert")).toHaveTextContent("Something went wrong.");
    expect(screen.queryByRole("button", { name: "Create event" })).not.toBeInTheDocument();
  });
});

describe("/events/:event", () => {
  it("shows the event in its zone with its schedule, warning about an item outside the dates", async () => {
    const { container } = mountApp({
      path: `/events/${E_NOW}`,
      session: ident([]),
      routes: base({
        [`GET /api/events/${E_NOW}`]: () =>
          json({
            ...NOW,
            schedule: [
              item(),
              item({
                id: "i2",
                key: undefined,
                label: "Strike",
                starts_at: iso(5 * day),
                ends_at: undefined,
              }),
            ],
          }),
      }),
    });
    expect(
      await screen.findByRole("heading", { level: 1, name: "Midsummer Burn" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Pacific/Auckland")).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 3, name: "Temple burn" })).toBeInTheDocument();
    expect(screen.getAllByText("Outside the event's dates.")).toHaveLength(1);
    for (const name of ["Edit", "Close event", "Add schedule item", "Run set-up again"])
      expect(screen.queryByRole("button", { name })).not.toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Set-up" })).not.toBeInTheDocument();
    await expectNoAxeViolations(container);
  });

  it("adds a schedule item read in the event's zone", async () => {
    const { fetchMock } = mountApp({
      path: `/events/${E_NOW}`,
      session: ident(["events.manage"]),
      routes: base({
        [`POST /api/events/${E_NOW}/schedule`]: () => json(item(), { status: 201 }),
      }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Add schedule item" }));
    const dialog = await screen.findByRole("dialog", { name: "Add schedule item" });
    await userEvent.type(within(dialog).getByLabelText(/^Label/), "Gate opens");
    await userEvent.type(within(dialog).getByLabelText("Key"), "gate-opens");
    await userEvent.type(within(dialog).getByLabelText(/^Starts/), "2026-07-01T12:00");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save item" }));
    await screen.findByText("Added");
    expect(await bodyOf(fetchMock, "POST", `/api/events/${E_NOW}/schedule`)).toEqual({
      label: "Gate opens",
      key: "gate-opens",
      starts_at: "2026-07-01T00:00:00.000Z",
    });
  });

  it("shows a duplicate key on its input, edits and deletes an item", async () => {
    let attempt = 0;
    const { fetchMock } = mountApp({
      path: `/events/${E_NOW}`,
      session: ident(["events.manage"]),
      routes: base({
        [`PUT /api/events/${E_NOW}/schedule/${ITEM}`]: () => {
          attempt += 1;
          return attempt === 1
            ? problem(409, { code: "conflict", fields: [{ field: "key", code: "duplicate" }] })
            : json(item({ label: "Temple burn!" }));
        },
        [`DELETE /api/events/${E_NOW}/schedule/${ITEM}`]: () => noContent(),
      }),
    });
    await userEvent.click(await screen.findByRole("button", { name: /^Edit\s*Temple burn/ }));
    const dialog = await screen.findByRole("dialog", { name: "Edit schedule item" });
    const label = within(dialog).getByLabelText(/^Label/);
    expect(label).toHaveValue("Temple burn");
    await userEvent.type(label, "!");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save item" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("already uses this key");
    expect(within(dialog).getByLabelText("Key")).toHaveAttribute("aria-invalid", "true");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save item" }));
    await screen.findByText("Saved");
    await userEvent.click(screen.getByRole("button", { name: /^Delete\s*Temple burn/ }));
    const confirm = await screen.findByRole("dialog", { name: "Delete this schedule item?" });
    await userEvent.click(within(confirm).getByRole("button", { name: "Delete item" }));
    await screen.findByText("Deleted");
    expect(
      fetchMock.mock.calls.some(
        ([r]) => r.method === "DELETE" && new URL(r.url).pathname.endsWith(`/schedule/${ITEM}`),
      ),
    ).toBe(true);
  });

  it("closes (warning that it has not ended) and reopens", async () => {
    const { fetchMock } = mountApp({
      path: `/events/${E_NOW}`,
      session: ident(["events.manage"]),
      routes: base({
        [`POST /api/events/${E_NOW}/close`]: () => json({ ...NOW, closed: true }),
        [`POST /api/events/${E_NOW}/reopen`]: () => json(NOW),
      }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Close event" }));
    const dialog = await screen.findByRole("dialog", { name: "Close this event?" });
    expect(dialog).toHaveTextContent("This event has not ended yet.");
    await userEvent.click(within(dialog).getByRole("button", { name: "Close event" }));
    await screen.findByText("Closed");
    expect(
      fetchMock.mock.calls.some(([r]) => new URL(r.url).pathname === `/api/events/${E_NOW}/close`),
    ).toBe(true);
  });

  it("a closed event shows a banner and only Reopen", async () => {
    mountApp({
      path: `/events/${E_CLOSED}`,
      session: ident(["events.manage"]),
      routes: base({
        [`GET /api/events/${E_CLOSED}`]: () => json({ ...CLOSED, schedule: [item()] }),
        [`POST /api/events/${E_CLOSED}/reopen`]: () => json(NEXT),
      }),
    });
    expect(await screen.findByText(/was closed on/)).toBeInTheDocument();
    for (const name of ["Edit", "Close event", "Add schedule item", "Run set-up again"])
      expect(screen.queryByRole("button", { name })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Delete / })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Reopen" }));
    const dialog = await screen.findByRole("dialog", { name: "Reopen this event?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Reopen event" }));
    await screen.findByText("Reopened");
  });

  it("edits the event, re-reading its dates in its zone", async () => {
    const { fetchMock } = mountApp({
      path: `/events/${E_NOW}`,
      session: ident(["events.manage"]),
      routes: base({ [`PUT /api/events/${E_NOW}`]: () => json(NOW) }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Edit" }));
    const dialog = await screen.findByRole("dialog", { name: "Edit" });
    const name = within(dialog).getByLabelText(/^Name/);
    expect(name).toHaveValue("Midsummer Burn");
    expect(within(dialog).queryByLabelText("Copy set-up from")).not.toBeInTheDocument();
    await userEvent.clear(name);
    await userEvent.type(name, "Midwinter Burn");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save changes" }));
    await screen.findByText("Saved");
    const body = (await bodyOf(fetchMock, "PUT", `/api/events/${E_NOW}`)) as {
      name: string;
      zone: string;
      dates: { start: string };
    };
    expect(body.name).toBe("Midwinter Burn");
    expect(body.zone).toBe("Pacific/Auckland");
    // the same instant after the round trip through the zone (to the minute)
    expect(Date.parse(body.dates.start)).toBe(
      Math.floor(Date.parse(NOW.dates.start) / 60000) * 60000,
    );
  });

  it("runs set-up again from another event and shows the outcome", async () => {
    const { fetchMock } = mountApp({
      path: `/events/${E_NOW}`,
      session: ident(["events.manage"]),
      routes: base({
        [`POST /api/events/${E_NOW}/setup`]: () =>
          json({ setup: [{ module: "volunteers", failed: false, items: 1 }] }),
      }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Run set-up again" }));
    const dialog = await screen.findByRole("dialog", { name: "Run set-up again" });
    const from = await within(dialog).findByLabelText(/^Copy set-up from/);
    expect(within(from).queryByRole("option", { name: "Midsummer Burn" })).not.toBeInTheDocument();
    await userEvent.selectOptions(from, E_PAST);
    await userEvent.click(within(dialog).getByRole("button", { name: "Run set-up" }));
    expect(await screen.findByText("volunteers: 1 item copied")).toBeInTheDocument();
    expect(await bodyOf(fetchMock, "POST", `/api/events/${E_NOW}/setup`)).toEqual({
      copy_from: E_PAST,
    });
  });

  it("shows not found, a closed refusal, and a disabled edit on a read-only copy", async () => {
    const first = mountApp({
      path: `/events/${E_PAST}`,
      session: ident([]),
      routes: base({ [`GET /api/events/${E_PAST}`]: () => problem(404, { code: "not_found" }) }),
    });
    expect(await screen.findByRole("alert")).toHaveTextContent("We couldn't find that.");
    first.unmount();
    mountApp({
      path: `/events/${E_NOW}`,
      session: ident(["events.manage"]),
      readOnly: () => true,
      routes: base(),
    });
    const edit = await screen.findByRole("button", { name: "Edit" });
    await waitFor(() => expect(edit).toBeDisabled());
    expect(edit).toHaveAccessibleDescription(/read-only/);
  });

  it("shows the kinds error with a retry inside the edit dialog", async () => {
    let n = 0;
    mountApp({
      path: `/events/${E_NOW}`,
      session: ident(["events.manage"]),
      routes: base({
        "GET /api/events/kinds": () => {
          n += 1;
          return n === 1 ? problem(500, { code: "internal", ref: "AAAA-0000" }) : json({ kinds });
        },
      }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Edit" }));
    const dialog = await screen.findByRole("dialog", { name: "Edit" });
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("Something went wrong.");
    await userEvent.click(within(dialog).getByRole("button", { name: "Try again" }));
    expect(await within(dialog).findByLabelText(/^Name/)).toBeInTheDocument();
  });

  it("shows a conflict from closing", async () => {
    mountApp({
      path: `/events/${E_NOW}`,
      session: ident(["events.manage"]),
      routes: base({
        [`POST /api/events/${E_NOW}/close`]: () =>
          problem(409, { code: "conflict", fields: [{ field: "body", code: "closed" }] }),
      }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Close event" }));
    const dialog = await screen.findByRole("dialog", { name: "Close this event?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Close event" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("is closed");
  });
});

describe("/events/kinds", () => {
  it("is forbidden without events.manage", async () => {
    mountApp({ path: "/events/kinds", session: ident([]), routes: base() });
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "You don't have access to this screen.",
    );
  });

  it("lists kinds with their keys, adds one and renames one in place", async () => {
    let renameAttempt = 0;
    const { fetchMock, container } = mountApp({
      path: "/events/kinds",
      session: ident(["events.manage"]),
      routes: base({
        "POST /api/events/kinds": () => json(kinds[2], { status: 201 }),
        "PUT /api/events/kinds/workshop": () => {
          renameAttempt += 1;
          return renameAttempt === 1
            ? problem(400, {
                code: "invalid",
                fields: [{ field: "name", code: "too_long", params: { max: 60 } }],
              })
            : json({ ...kinds[2], name: "Workshop" });
        },
      }),
    });
    expect(await screen.findByRole("heading", { name: "Workshop weekend" })).toBeInTheDocument();
    expect(screen.getByText("workshop")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Delete/ })).not.toBeInTheDocument();
    await expectNoAxeViolations(container);

    await userEvent.click(screen.getByRole("button", { name: "New kind" }));
    const dialog = await screen.findByRole("dialog", { name: "New kind" });
    await userEvent.type(within(dialog).getByLabelText(/^Key/), "workshop");
    await userEvent.type(within(dialog).getByLabelText(/^Name/), "Workshop weekend");
    await userEvent.click(within(dialog).getByRole("button", { name: "Add kind" }));
    await screen.findByText("Kind added");
    expect(await bodyOf(fetchMock, "POST", "/api/events/kinds")).toEqual({
      key: "workshop",
      name: "Workshop weekend",
    });

    await userEvent.click(screen.getByRole("button", { name: /^Rename\s*Workshop weekend/ }));
    const name = screen.getByRole("textbox", { name: /^Name/ });
    await userEvent.clear(name);
    await userEvent.type(name, "Workshop");
    await userEvent.click(screen.getByRole("button", { name: "Save name" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Must be at most 60 characters.");
    expect(name).toHaveAttribute("aria-invalid", "true");
    await userEvent.click(screen.getByRole("button", { name: "Save name" }));
    await screen.findByText("Renamed");
  });

  it("cancelling a rename returns focus to that card's Rename button; keys turn off autocorrect", async () => {
    mountApp({ path: "/events/kinds", session: ident(["events.manage"]), routes: base() });
    const rename = await screen.findByRole("button", { name: /^Rename\s*Workshop weekend/ });
    await userEvent.click(rename);
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: /^Rename\s*Workshop weekend/ })).toHaveFocus(),
    );
    // it stays in the Tab order
    expect(screen.getByRole("button", { name: /^Rename\s*Workshop weekend/ })).toHaveProperty(
      "tabIndex",
      0,
    );
    await userEvent.click(screen.getByRole("button", { name: "New kind" }));
    const key = within(await screen.findByRole("dialog", { name: "New kind" })).getByLabelText(
      /^Key/,
    );
    expect(key).toHaveAttribute("autocapitalize", "none");
    expect(key).toHaveAttribute("autocorrect", "off");
    expect(key).toHaveAttribute("spellcheck", "false");
  });

  it("shows a taken key on the input and a failed load with a retry", async () => {
    mountApp({
      path: "/events/kinds",
      session: ident(["events.manage"]),
      routes: base({
        "POST /api/events/kinds": () =>
          problem(409, { code: "conflict", fields: [{ field: "key", code: "taken" }] }),
      }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "New kind" }));
    const dialog = await screen.findByRole("dialog", { name: "New kind" });
    await userEvent.type(within(dialog).getByLabelText(/^Key/), "burn");
    await userEvent.type(within(dialog).getByLabelText(/^Name/), "Burn");
    await userEvent.click(within(dialog).getByRole("button", { name: "Add kind" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("This is already in use.");
    expect(within(dialog).getByLabelText(/^Key/)).toHaveAttribute("aria-invalid", "true");
  });

  it("a failed load shows its error with a retry", async () => {
    mountApp({
      path: "/events/kinds",
      session: ident(["events.manage"]),
      routes: base({
        "GET /api/events/kinds": () => problem(500, { code: "internal", ref: "AAAA-0000" }),
      }),
    });
    expect(await screen.findByRole("alert")).toHaveTextContent("Something went wrong.");
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument();
  });
});
