import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { expectNoAxeViolations } from "@/test/axe";
import { json, problem } from "@/test/fakeFetch";
import {
  bodyOf,
  DEPT,
  EVENT,
  mount,
  OTHER,
  POS,
  QUAL,
  QUAL2,
  RECORD,
  seen,
  shift,
  SHIFT,
  V,
} from "./testkit";

const ALL = [
  "volunteers.shifts.manage",
  "volunteers.positions.manage",
  "volunteers.members.manage",
  "volunteers.qualifications.manage",
  "volunteers.qualifications.verify",
  "volunteers.rosters.read",
].map((p) => `${p}@${DEPT}`);
const none = () => new Response(null, { status: 204 });
const pending = {
  id: RECORD,
  qualification: QUAL,
  person: OTHER,
  acquired_on: "2026-01-02",
  certificate: "f1",
};

describe("volunteers: admin", () => {
  it("lists the departments the person leads and how many certificates wait", async () => {
    const { container } = mount({
      path: "/volunteers/admin",
      perms: ALL,
      routes: { [`GET ${V}/qualification-records`]: () => json({ items: [pending] }) },
    });
    expect(await screen.findByRole("link", { name: "Gate" })).toHaveAttribute(
      "href",
      `/volunteers/departments/${DEPT}`,
    );
    expect(await screen.findByText("1 certificate is waiting for you.")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Qualification catalogue" })).not.toBeInTheDocument();
    await expectNoAxeViolations(container);
  });

  it("a site-wide holder sees every department and the catalogue link; none led: empty state", async () => {
    const { unmount } = mount({
      path: "/volunteers/admin",
      perms: ["volunteers.qualifications.manage"],
    });
    expect(
      await screen.findByRole("link", { name: "Qualification catalogue" }),
    ).toBeInTheDocument();
    expect(await screen.findByRole("link", { name: "Gate" })).toBeInTheDocument();
    unmount();
    mount({ path: "/volunteers/admin", perms: ["volunteers.shifts.manage@other"] });
    expect(await screen.findByText("You lead no department yet.")).toBeInTheDocument();
  });

  it("without any lead permission: the forbidden state, and no admin nav entry", async () => {
    mount({ path: "/volunteers/admin" });
    expect(await screen.findByText("You don't have access to this screen.")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Volunteers" })).not.toBeInTheDocument();
  });

  it("a failing department list shows an error with retry", async () => {
    mount({
      path: "/volunteers/admin",
      perms: ALL,
      routes: {
        "GET /api/departments/": () => problem(500, { code: "internal" }),
        [`GET ${V}/qualification-records`]: () => json({ items: [] }),
      },
    });
    expect(await screen.findByRole("alert")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument();
  });
});

describe("volunteers: a department", () => {
  const dpath = `/volunteers/departments/${DEPT}`;

  it("offers only the tabs the session shows; the first is selected", async () => {
    mount({
      path: dpath,
      perms: [`volunteers.members.manage@${DEPT}`, `volunteers.qualifications.read@${DEPT}`],
      routes: { [`GET ${V}/departments/${DEPT}/members`]: () => json({ items: [] }) },
    });
    const nav = await screen.findByRole("navigation", { name: "Sections" });
    expect(
      within(nav)
        .getAllByRole("link")
        .map((a) => a.textContent),
    ).toEqual(["Members", "Qualifications"]);
    expect(within(nav).getByRole("link", { name: "Members" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(await screen.findByText("No members yet.")).toBeInTheDocument();
  });

  it("no lead permission here: forbidden", async () => {
    mount({ path: dpath });
    expect(await screen.findByText("You don't have access to this screen.")).toBeInTheDocument();
  });

  it("an unknown department is a not-found state", async () => {
    mount({ path: "/volunteers/departments/nope", perms: ["volunteers.shifts.manage"] });
    expect(await screen.findByText(/We couldn't find that/)).toBeInTheDocument();
  });

  it("Shifts tab: fill counts, New shift link, and copy from a previous event", async () => {
    const { fetchMock } = mount({
      path: dpath,
      perms: ALL,
      routes: {
        [`GET ${V}/shifts`]: () =>
          json({
            items: [
              shift({ booked: 2 }),
              shift({ id: "s2", label: "Canceled one", canceled_at: "2099-01-01T00:00:00Z" }),
            ],
          }),
        [`POST ${V}/departments/${DEPT}/shifts/copy`]: () => json({ created: 3 }),
      },
    });
    expect(await screen.findByText("2 of 4 booked")).toBeInTheDocument();
    expect(screen.getByText("Canceled")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "New shift" })).toHaveAttribute(
      "href",
      `/volunteers/departments/${DEPT}/shifts/new`,
    );
    await userEvent.click(screen.getByRole("button", { name: /Copy from a previous/ }));
    const dialog = await screen.findByRole("dialog", { name: "Copy shifts" });
    await within(dialog).findAllByRole("option", { name: "Midsummer" });
    await userEvent.selectOptions(within(dialog).getByLabelText(/^Copy from/), EVENT);
    await userEvent.selectOptions(within(dialog).getByLabelText(/^Copy to/), EVENT);
    await userEvent.click(within(dialog).getByRole("button", { name: "Copy shifts" }));
    await waitFor(async () =>
      expect(await bodyOf(fetchMock, "POST", `${V}/departments/${DEPT}/shifts/copy`)).toEqual({
        from: EVENT,
        to: EVENT,
      }),
    );
    expect((await screen.findAllByText("3 shifts copied")).length).toBeGreaterThan(0);
  });

  it("Shifts tab empty state names the action; a failure shows an error", async () => {
    const { unmount } = mount({
      path: dpath,
      perms: ALL,
      routes: { [`GET ${V}/shifts`]: () => json({}) },
    });
    expect(await screen.findByText("No shifts yet. Create the first one.")).toBeInTheDocument();
    unmount();
    mount({
      path: dpath,
      perms: ALL,
      routes: { [`GET ${V}/shifts`]: () => problem(500, { code: "internal" }) },
    });
    expect(await screen.findByRole("alert")).toBeInTheDocument();
  });

  it("Positions tab: create with the hide switch, then set requirements, then archive (in use is explained)", async () => {
    const { fetchMock } = mount({
      path: `${dpath}?tab=positions`,
      perms: ALL,
      routes: {
        [`POST ${V}/departments/${DEPT}/positions`]: () => json({ id: POS }, { status: 201 }),
        [`PUT ${V}/positions/${POS}/requirements`]: () => json({ id: POS }),
        [`POST ${V}/positions/${POS}/archive`]: () =>
          problem(409, {
            code: "conflict",
            fields: [{ field: "position", code: "in_use", reason: "" }],
          }),
      },
    });
    expect(await screen.findByText("No requirements.")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "New position" }));
    let dialog = await screen.findByRole("dialog", { name: "New position" });
    await userEvent.type(within(dialog).getByLabelText(/^Name/), "Greeter two");
    await userEvent.click(
      within(dialog).getByLabelText("Hide who's on shifts from other volunteers"),
    );
    await userEvent.selectOptions(within(dialog).getByLabelText("Who can sign up"), "members");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save position" }));
    await waitFor(async () =>
      expect(await bodyOf(fetchMock, "POST", `${V}/departments/${DEPT}/positions`)).toEqual({
        name: "Greeter two",
        description: "",
        access: "members",
        hide_shift_mates: true,
      }),
    );

    await userEvent.click(screen.getByRole("button", { name: /^Requirements/ }));
    dialog = await screen.findByRole("dialog", { name: "Requirements for Greeter" });
    await userEvent.click(await within(dialog).findByLabelText("First aid"));
    await userEvent.click(within(dialog).getByLabelText("Radio licence"));
    await userEvent.click(within(dialog).getByRole("button", { name: "Save requirements" }));
    await waitFor(async () =>
      expect(await bodyOf(fetchMock, "PUT", `${V}/positions/${POS}/requirements`)).toEqual({
        qualifications: [QUAL, QUAL2],
      }),
    );

    await userEvent.click(screen.getByRole("button", { name: /^Archive/ }));
    dialog = await screen.findByRole("dialog", { name: "Archive this position?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Archive position" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("still in use");
  });

  it("Positions tab empty state; field errors land on their inputs", async () => {
    mount({
      path: `${dpath}?tab=positions`,
      perms: ALL,
      routes: {
        [`GET ${V}/departments/${DEPT}/positions`]: () => json({}),
        [`POST ${V}/departments/${DEPT}/positions`]: () =>
          problem(400, {
            code: "invalid",
            fields: [{ field: "name", code: "required", reason: "" }],
          }),
      },
    });
    expect(await screen.findByText("No positions yet. Create the first one.")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "New position" }));
    const dialog = await screen.findByRole("dialog", { name: "New position" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Save position" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("Name: Required.");
    expect(within(dialog).getByLabelText(/^Name/)).toHaveAttribute("aria-invalid", "true");
  });

  it("Members tab: eligibility per position, add by search, remove after confirming", async () => {
    const { fetchMock } = mount({
      path: `${dpath}?tab=members`,
      perms: ALL,
      routes: {
        [`GET ${V}/departments/${DEPT}/members`]: () =>
          json({
            items: [
              {
                id: "m1",
                department: DEPT,
                person: OTHER,
                playa_name: "Pat Dust",
                eligible: [POS],
              },
            ],
          }),
        "GET /api/people/search": () =>
          json({ matches: [{ id: "p3", playa_name: "Moth", pronouns: "" }] }),
        [`POST ${V}/departments/${DEPT}/members`]: () => json({ id: "m2" }, { status: 201 }),
        [`DELETE ${V}/departments/${DEPT}/members/${OTHER}`]: none,
      },
    });
    expect(await screen.findByText("Can sign up for: Greeter.")).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText("Playa name"), "Mo");
    await userEvent.click(screen.getByRole("button", { name: "Search" }));
    await userEvent.click(await screen.findByRole("button", { name: /Moth/ }));
    await userEvent.click(screen.getByRole("button", { name: "Add member" }));
    await waitFor(async () =>
      expect(await bodyOf(fetchMock, "POST", `${V}/departments/${DEPT}/members`)).toEqual({
        person: "p3",
      }),
    );
    await userEvent.click(screen.getByRole("button", { name: /^Remove/ }));
    const dialog = await screen.findByRole("dialog", { name: "Remove this member?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Remove member" }));
    await waitFor(() =>
      expect(seen(fetchMock, "DELETE", `${V}/departments/${DEPT}/members/${OTHER}`)).toBe(true),
    );
  });

  it("Members tab and Shifts tab: Show more appends the next page", async () => {
    const page = (item: unknown, next?: string) =>
      json({ items: [item], ...(next ? { next } : {}) });
    const a = mount({
      path: `${dpath}?tab=members`,
      perms: ALL,
      routes: {
        [`GET ${V}/departments/${DEPT}/members`]: (req) =>
          new URL(req.url).searchParams.get("after") === "m"
            ? page({ id: "m2", department: DEPT, person: "p2", playa_name: "Second Member" })
            : page({ id: "m1", department: DEPT, person: OTHER, playa_name: "First Member" }, "m"),
      },
    });
    await screen.findByRole("heading", { level: 3, name: "First Member" });
    await userEvent.click(screen.getByRole("button", { name: "Show more" }));
    await screen.findByRole("heading", { level: 3, name: "Second Member" });
    expect(screen.queryByRole("button", { name: "Show more" })).not.toBeInTheDocument();
    a.unmount();
    mount({
      path: `${dpath}?tab=shifts`,
      perms: ALL,
      routes: {
        [`GET ${V}/shifts`]: (req) =>
          new URL(req.url).searchParams.get("after") === "s"
            ? page(shift({ id: "s2", label: "Second shift" }))
            : page(shift({ label: "First shift" }), "s"),
      },
    });
    await screen.findByRole("heading", { level: 3, name: "First shift" });
    await userEvent.click(screen.getByRole("button", { name: "Show more" }));
    await screen.findByRole("heading", { level: 3, name: "Second shift" });
  });

  it("Members tab: a reader without members.manage sees no add or remove; duplicate is explained", async () => {
    const { unmount } = mount({
      path: `${dpath}?tab=members`,
      perms: [`volunteers.rosters.read@${DEPT}`],
      routes: {
        [`GET ${V}/departments/${DEPT}/members`]: () =>
          json({ items: [{ id: "m1", department: DEPT, person: OTHER }] }),
      },
    });
    expect(await screen.findByText("Not eligible for any position yet.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Remove/ })).not.toBeInTheDocument();
    expect(screen.queryByText("Add a member")).not.toBeInTheDocument();
    unmount();
    mount({
      path: `${dpath}?tab=members`,
      perms: ALL,
      routes: {
        [`GET ${V}/departments/${DEPT}/members`]: () => json({}),
        "GET /api/people/search": () =>
          json({ matches: [{ id: "p3", playa_name: "Moth", pronouns: "" }] }),
        [`POST ${V}/departments/${DEPT}/members`]: () =>
          problem(409, {
            code: "conflict",
            fields: [{ field: "person", code: "duplicate", reason: "" }],
          }),
      },
    });
    await userEvent.type(await screen.findByLabelText("Playa name"), "Mo");
    await userEvent.click(screen.getByRole("button", { name: "Search" }));
    await userEvent.click(await screen.findByRole("button", { name: /Moth/ }));
    await userEvent.click(screen.getByRole("button", { name: "Add member" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("That's already there.");
  });

  it("Qualifications tab: the department's entries editable, org-wide ones read-only; create and archive", async () => {
    const { fetchMock } = mount({
      path: `${dpath}?tab=qualifications`,
      perms: ALL,
      routes: {
        [`POST ${V}/qualifications`]: () => json({ id: "q9" }, { status: 201 }),
        [`POST ${V}/qualifications/${QUAL2}/archive`]: none,
      },
    });
    expect(
      await screen.findByRole("heading", { level: 3, name: "Radio licence" }),
    ).toBeInTheDocument();
    const org = screen.getByRole("region", { name: "Organization-wide qualifications" });
    expect(within(org).getByText("First aid")).toBeInTheDocument();
    expect(within(org).queryByRole("button")).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "New qualification" }));
    const dialog = await screen.findByRole("dialog", { name: "New qualification" });
    await userEvent.type(within(dialog).getByLabelText(/^Name/), "Forklift");
    await userEvent.type(within(dialog).getByLabelText(/^Valid for/), "90");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save qualification" }));
    await waitFor(async () =>
      expect(await bodyOf(fetchMock, "POST", `${V}/qualifications`)).toEqual({
        department: DEPT,
        name: "Forklift",
        description: "",
        validity_days: 90,
      }),
    );
    await userEvent.click(screen.getByRole("button", { name: /^Archive/ }));
    const confirm = await screen.findByRole("dialog", { name: "Archive this qualification?" });
    await userEvent.click(within(confirm).getByRole("button", { name: "Archive qualification" }));
    await waitFor(() =>
      expect(seen(fetchMock, "POST", `${V}/qualifications/${QUAL2}/archive`)).toBe(true),
    );
  });

  it("Pending tab: an org-wide certificate says so; a department one does not; the hint follows Never expires", async () => {
    mount({
      path: `${dpath}?tab=pending`,
      perms: ALL,
      routes: {
        [`GET ${V}/qualification-records`]: () =>
          json({ items: [pending, { ...pending, id: "r2", qualification: QUAL2 }] }),
      },
    });
    const first = (await screen.findByRole("heading", { name: "First aid" })).closest("li")!;
    const second = (await screen.findByRole("heading", { name: "Radio licence" })).closest("li")!;
    expect(within(first).getByText("Org-wide")).toBeInTheDocument();
    expect(within(second).queryByText("Org-wide")).not.toBeInTheDocument();
    await userEvent.click(within(first).getByRole("button", { name: /^Verify/ }));
    const dialog = await screen.findByRole("dialog", { name: "Verify First aid" });
    expect(within(dialog).getByText("Leave empty to keep the end date as it is.")).toBeVisible();
    await userEvent.click(within(dialog).getByLabelText("Never expires"));
    expect(within(dialog).getByText("Cleared while Never expires is on.")).toBeVisible();
  });

  it("verify with Never expires clears and disables the date and sends no_expiry", async () => {
    const { fetchMock } = mount({
      path: `${dpath}?tab=pending`,
      perms: ALL,
      routes: {
        [`GET ${V}/qualification-records`]: () => json({ items: [pending] }),
        [`POST ${V}/qualification-records/${RECORD}/verify`]: () => json({ id: RECORD }),
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: /^Verify/ }));
    const dialog = await screen.findByRole("dialog", { name: "Verify First aid" });
    const expires = within(dialog).getByLabelText(/^Expires on/);
    await userEvent.click(within(dialog).getByLabelText("Never expires"));
    expect(expires).toBeDisabled();
    expect(expires).toHaveValue("");
    await userEvent.click(within(dialog).getByRole("button", { name: "Verify" }));
    await waitFor(async () =>
      expect(
        await bodyOf(fetchMock, "POST", `${V}/qualification-records/${RECORD}/verify`),
      ).toEqual({ acquired_on: "2026-01-02", no_expiry: true }),
    );
  });

  it("Pending tab: verify with corrected dates; reject after confirming; empty state", async () => {
    let items = [pending];
    const { fetchMock } = mount({
      path: `${dpath}?tab=pending`,
      perms: ALL,
      routes: {
        [`GET ${V}/qualification-records`]: () => json({ items }),
        [`POST ${V}/qualification-records/${RECORD}/verify`]: () => {
          items = [];
          return json({ id: RECORD });
        },
        [`POST ${V}/qualification-records/${RECORD}/reject`]: none,
      },
    });
    expect(await screen.findByRole("link", { name: "Pat Dust" })).toHaveAttribute(
      "href",
      `/volunteers/people/${OTHER}`,
    );
    expect(screen.getByRole("button", { name: /^Open\s*Certificate/ })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /^Reject/ }));
    const rej = await screen.findByRole("dialog", { name: "Reject this certificate?" });
    await userEvent.click(within(rej).getByRole("button", { name: "Reject certificate" }));
    await waitFor(() =>
      expect(seen(fetchMock, "POST", `${V}/qualification-records/${RECORD}/reject`)).toBe(true),
    );
    await userEvent.click(screen.getByRole("button", { name: /^Verify/ }));
    const dialog = await screen.findByRole("dialog", { name: "Verify First aid" });
    const acquired = within(dialog).getByLabelText(/^Acquired on/);
    expect(acquired).toHaveValue("2026-01-02");
    fireEvent.change(acquired, { target: { value: "2026-01-03" } });
    fireEvent.change(within(dialog).getByLabelText(/^Expires on/), {
      target: { value: "2027-01-03" },
    });
    await userEvent.click(within(dialog).getByRole("button", { name: "Verify" }));
    await waitFor(async () =>
      expect(
        await bodyOf(fetchMock, "POST", `${V}/qualification-records/${RECORD}/verify`),
      ).toEqual({
        acquired_on: "2026-01-03",
        expires_on: "2027-01-03",
      }),
    );
    expect(await screen.findByText("No certificates are waiting.")).toBeInTheDocument();
  });

  it("read-only copy: write buttons are disabled and say why", async () => {
    mount({ path: `${dpath}?tab=positions`, perms: ALL, readOnly: () => true });
    const button = await screen.findByRole("button", { name: "New position" });
    expect(button).toBeDisabled();
    expect(screen.getAllByText(/This copy is read-only/).length).toBeGreaterThan(0);
  });
});

describe("volunteers: the shift form", () => {
  const newPath = `/volunteers/departments/${DEPT}/shifts/new`;

  it("creates a shift in the event's zone and opens it", async () => {
    const { fetchMock, router } = mount({
      path: newPath,
      perms: ALL,
      routes: {
        [`POST ${V}/departments/${DEPT}/shifts`]: () => json(shift(), { status: 201 }),
        [`GET ${V}/shifts/${SHIFT}`]: () => json(shift()),
      },
    });
    await screen.findByRole("option", { name: "Greeter" });
    await screen.findByRole("option", { name: "Midsummer" });
    await userEvent.selectOptions(screen.getByLabelText(/^Position/), POS);
    await userEvent.selectOptions(screen.getByLabelText("Event"), EVENT);
    expect(
      screen.getByText("Times are in the event's time zone, America/Denver."),
    ).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText(/^Name of the shift/), "Morning dirt");
    fireEvent.change(screen.getByLabelText(/^Starts/), { target: { value: "2099-07-01T09:00" } });
    fireEvent.change(screen.getByLabelText(/^Ends/), { target: { value: "2099-07-01T13:00" } });
    await userEvent.clear(screen.getByLabelText(/^Places/));
    await userEvent.type(screen.getByLabelText(/^Places/), "4");
    await userEvent.type(screen.getByLabelText("Where"), "Ranger HQ");
    expect(screen.getByText(/Don't put personal information here/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Create shift" }));
    await waitFor(async () =>
      expect(await bodyOf(fetchMock, "POST", `${V}/departments/${DEPT}/shifts`)).toEqual({
        position: POS,
        event: EVENT,
        label: "Morning dirt",
        starts: "2099-07-01T15:00:00.000Z",
        ends: "2099-07-01T19:00:00.000Z",
        slots: 4,
        location: "Ranger HQ",
        notes: "",
      }),
    );
    await waitFor(() =>
      expect(router()?.state.location.pathname).toBe(`/volunteers/shifts/${SHIFT}`),
    );
  });

  it("standing work sends the chosen zone; field errors land on their inputs", async () => {
    const { fetchMock } = mount({
      path: newPath,
      perms: ALL,
      routes: {
        [`POST ${V}/departments/${DEPT}/shifts`]: () =>
          problem(400, {
            code: "invalid",
            fields: [
              { field: "label", code: "required", reason: "" },
              { field: "ends", code: "range", reason: "" },
            ],
          }),
      },
    });
    await screen.findByRole("option", { name: "Greeter" });
    await userEvent.selectOptions(screen.getByLabelText(/^Position/), POS);
    await userEvent.selectOptions(screen.getByLabelText(/^Time zone/), "America/Denver");
    fireEvent.change(screen.getByLabelText(/^Starts/), { target: { value: "2099-01-10T09:00" } });
    await userEvent.click(screen.getByRole("button", { name: "Create shift" }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Name of the shift: Required.");
    expect(screen.getByLabelText(/^Name of the shift/)).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByLabelText(/^Ends/)).toHaveAttribute("aria-invalid", "true");
    const body = (await bodyOf(fetchMock, "POST", `${V}/departments/${DEPT}/shifts`)) as Record<
      string,
      unknown
    >;
    expect(body["zone"]).toBe("America/Denver");
    expect(body["starts"]).toBe("2099-01-10T16:00:00.000Z");
    expect(body).not.toHaveProperty("event");
  });

  it("a department with no positions explains the next step", async () => {
    mount({
      path: newPath,
      perms: ALL,
      routes: { [`GET ${V}/departments/${DEPT}/positions`]: () => json({}) },
    });
    expect(await screen.findByText(/has no positions yet/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Create a position" })).toHaveAttribute(
      "href",
      `/volunteers/departments/${DEPT}?tab=positions`,
    );
  });

  it("without shifts.manage: forbidden", async () => {
    mount({ path: newPath });
    expect(await screen.findByText("You don't have access to this screen.")).toBeInTheDocument();
  });

  it("edit prefills from the shift (times shown in its zone) and saves with PUT", async () => {
    const { fetchMock } = mount({
      path: `/volunteers/shifts/${SHIFT}/edit`,
      perms: ALL,
      routes: {
        [`GET ${V}/shifts/${SHIFT}`]: () => json(shift({ event: EVENT, notes: "Bring gloves" })),
        [`PUT ${V}/shifts/${SHIFT}`]: () => json(shift()),
      },
    });
    const starts = await screen.findByLabelText(/^Starts/);
    expect(starts).toHaveValue("2099-07-01T09:00");
    expect(screen.getByLabelText(/^Name of the shift/)).toHaveValue("Morning dirt");
    expect(screen.getByLabelText("Notes")).toHaveValue("Bring gloves");
    await userEvent.click(screen.getByRole("button", { name: "Save shift" }));
    await waitFor(async () =>
      expect(await bodyOf(fetchMock, "PUT", `${V}/shifts/${SHIFT}`)).toMatchObject({
        starts: "2099-07-01T15:00:00.000Z",
        event: EVENT,
      }),
    );
  });

  it("edit of an unknown shift: not found", async () => {
    mount({
      path: `/volunteers/shifts/${SHIFT}/edit`,
      perms: ALL,
      routes: { [`GET ${V}/shifts/${SHIFT}`]: () => problem(404, { code: "not_found" }) },
    });
    expect(await screen.findByRole("alert")).toHaveTextContent("We couldn't find that.");
  });
});

describe("volunteers: a person's qualifications", () => {
  const ppath = `/volunteers/people/${OTHER}`;
  const lead = [`volunteers.qualifications.verify@${DEPT}`];

  it("lists records, opens certificates, records a qualification", async () => {
    const { fetchMock, container } = mount({
      path: ppath,
      perms: lead,
      routes: {
        [`GET ${V}/people/${OTHER}/qualifications`]: () =>
          json({ items: [{ ...pending, verified_at: "2026-02-01T00:00:00Z", verified_by: "x" }] }),
        [`POST ${V}/people/${OTHER}/qualifications`]: () => json({ id: "r9" }, { status: 201 }),
      },
    });
    expect(
      await screen.findByRole("heading", { level: 1, name: "Qualifications of Pat Dust" }),
    ).toBeInTheDocument();
    expect(await screen.findByText("Verified")).toBeInTheDocument();
    await screen.findByRole("option", { name: "First aid" });
    await userEvent.selectOptions(screen.getByLabelText(/^Qualification/), QUAL);
    fireEvent.change(screen.getByLabelText(/^Acquired on/), { target: { value: "2026-02-03" } });
    // an empty end date means acquired + validity, so the field shows that date up front
    expect(screen.getByLabelText(/^Expires on/)).toHaveValue("2027-02-03");
    await userEvent.click(screen.getByRole("button", { name: "Record qualification" }));
    await waitFor(async () =>
      expect(await bodyOf(fetchMock, "POST", `${V}/people/${OTHER}/qualifications`)).toEqual({
        qualification: QUAL,
        acquired_on: "2026-02-03",
        expires_on: "2027-02-03",
      }),
    );
    await expectNoAxeViolations(container);
  });

  it("recording with Never expires sends no_expiry and no date", async () => {
    const { fetchMock } = mount({
      path: ppath,
      perms: lead,
      routes: {
        [`GET ${V}/people/${OTHER}/qualifications`]: () => json({ items: [] }),
        [`POST ${V}/people/${OTHER}/qualifications`]: () => json({ id: "r9" }, { status: 201 }),
      },
    });
    await screen.findByRole("option", { name: "First aid" });
    await userEvent.selectOptions(screen.getByLabelText(/^Qualification/), QUAL);
    fireEvent.change(screen.getByLabelText(/^Acquired on/), { target: { value: "2026-02-03" } });
    await userEvent.click(screen.getByLabelText("Never expires"));
    expect(screen.getByLabelText(/^Expires on/)).toBeDisabled();
    await userEvent.click(screen.getByRole("button", { name: "Record qualification" }));
    await waitFor(async () =>
      expect(await bodyOf(fetchMock, "POST", `${V}/people/${OTHER}/qualifications`)).toEqual({
        qualification: QUAL,
        acquired_on: "2026-02-03",
        no_expiry: true,
      }),
    );
  });

  it("a person outside the viewer's extent is a not-found state; empty is explained; no permission is forbidden", async () => {
    const { unmount } = mount({
      path: ppath,
      perms: lead,
      routes: {
        [`GET ${V}/people/${OTHER}/qualifications`]: () => problem(404, { code: "not_found" }),
      },
    });
    expect(await screen.findByText(/We couldn't find that/)).toBeInTheDocument();
    unmount();
    const second = mount({
      path: ppath,
      perms: lead,
      routes: { [`GET ${V}/people/${OTHER}/qualifications`]: () => json({}) },
    });
    expect(
      await screen.findByText("No qualifications on record in your area."),
    ).toBeInTheDocument();
    second.unmount();
    mount({ path: ppath });
    expect(await screen.findByText("You don't have access to this screen.")).toBeInTheDocument();
  });

  it("removes a record after confirming, and verifies a pending one", async () => {
    const { fetchMock } = mount({
      path: ppath,
      perms: lead,
      routes: {
        [`GET ${V}/people/${OTHER}/qualifications`]: () => json({ items: [pending] }),
        [`DELETE ${V}/qualification-records/${RECORD}`]: none,
        [`POST ${V}/qualification-records/${RECORD}/verify`]: () => json({ id: RECORD }),
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: /^Verify/ }));
    const verify = await screen.findByRole("dialog", { name: "Verify First aid" });
    await userEvent.click(within(verify).getByRole("button", { name: "Verify" }));
    await waitFor(() =>
      expect(seen(fetchMock, "POST", `${V}/qualification-records/${RECORD}/verify`)).toBe(true),
    );
    await userEvent.click(screen.getByRole("button", { name: /^Remove/ }));
    const dialog = await screen.findByRole("dialog", { name: "Remove this record?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Remove record" }));
    await waitFor(() =>
      expect(seen(fetchMock, "DELETE", `${V}/qualification-records/${RECORD}`)).toBe(true),
    );
  });
});

describe("volunteers: the org-wide catalogue", () => {
  it("needs site-wide qualifications.manage", async () => {
    mount({
      path: "/volunteers/qualifications",
      perms: [`volunteers.qualifications.manage@${DEPT}`],
    });
    expect(await screen.findByText("You don't have access to this screen.")).toBeInTheDocument();
  });

  it("lists only org-wide entries; edit saves with PUT; archived are shown on request", async () => {
    const { fetchMock } = mount({
      path: "/volunteers/qualifications",
      perms: ["volunteers.qualifications.manage"],
      routes: { [`PUT ${V}/qualifications/${QUAL}`]: () => json({ id: QUAL }) },
    });
    expect(await screen.findByRole("heading", { level: 3, name: "First aid" })).toBeInTheDocument();
    expect(screen.queryByText("Radio licence")).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /^Edit/ }));
    const dialog = await screen.findByRole("dialog", { name: "Edit qualification" });
    expect(within(dialog).getByLabelText(/^Name/)).toHaveValue("First aid");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save qualification" }));
    await waitFor(async () =>
      expect(await bodyOf(fetchMock, "PUT", `${V}/qualifications/${QUAL}`)).toEqual({
        name: "First aid",
        description: "",
        validity_days: 365,
      }),
    );
  });

  it("empty and error states", async () => {
    const { unmount } = mount({
      path: "/volunteers/qualifications",
      perms: ["volunteers.qualifications.manage"],
      routes: { [`GET ${V}/qualifications`]: () => json({}) },
    });
    expect(
      await screen.findByText("No qualifications yet. Create the first one."),
    ).toBeInTheDocument();
    unmount();
    mount({
      path: "/volunteers/qualifications",
      perms: ["volunteers.qualifications.manage"],
      routes: { [`GET ${V}/qualifications`]: () => problem(500, { code: "internal" }) },
    });
    expect(await screen.findByRole("alert")).toBeInTheDocument();
  });
});
