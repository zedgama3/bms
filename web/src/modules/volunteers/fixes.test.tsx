// Regression tests for the web-reviewer round on the volunteers screens.
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { json, problem } from "@/test/fakeFetch";
import { addDays } from "./helpers";
import { withRangeCodes } from "./rangeCodes";
import {
  bodyOf,
  DEPT,
  mount,
  ME,
  OTHER,
  POS,
  QUAL,
  QUAL2,
  queryOf,
  RECORD,
  seen,
  shift,
  SHIFT,
  SIGNUP,
  V,
} from "./testkit";
import { ApiError } from "@/api";

const none = () => new Response(null, { status: 204 });
const LEAD = [
  "volunteers.shifts.manage",
  "volunteers.positions.manage",
  "volunteers.members.manage",
  "volunteers.qualifications.manage",
  "volunteers.qualifications.verify",
  "volunteers.rosters.read",
].map((p) => `${p}@${DEPT}`);
const dpath = `/volunteers/departments/${DEPT}`;
const pendingRec = {
  id: RECORD,
  qualification: QUAL,
  person: OTHER,
  acquired_on: "2026-01-02",
  expires_on: "2027-01-02",
  certificate: "f1",
};
const invalid = (...fields: { field: string; code: string; params?: Record<string, string> }[]) =>
  problem(400, { code: "invalid", fields: fields.map((f) => ({ reason: "", ...f })) });

describe("volunteers: certificates", () => {
  it("the upload form does not force the camera (a PDF or a gallery photo can be chosen)", async () => {
    mount({
      path: "/volunteers/me",
      routes: {
        [`GET ${V}/me/schedule`]: () => json({}),
        [`GET ${V}/me/departments`]: () => json({}),
        [`GET ${V}/people/${ME}/qualifications`]: () => json({ items: [], self_upload: true }),
        [`GET ${V}/me/listing`]: () => json({ listed: true }),
      },
    });
    const input = await screen.findByLabelText(/^Certificate/);
    expect(input).not.toHaveAttribute("capture");
    expect(input).toHaveAttribute("accept", expect.stringContaining("application/pdf"));
  });

  it("verify: an empty end date is not sent and the hint says the record's date stays", async () => {
    const { fetchMock } = mount({
      path: `${dpath}?tab=pending`,
      perms: LEAD,
      routes: {
        [`GET ${V}/qualification-records`]: () => json({ items: [pendingRec] }),
        [`POST ${V}/qualification-records/${RECORD}/verify`]: () => json({ id: RECORD }),
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: /^Verify/ }));
    const dialog = await screen.findByRole("dialog", { name: "Verify First aid" });
    expect(within(dialog).getByText("Leave empty to keep the end date as it is.")).toBeVisible();
    expect(within(dialog).queryByText(/does not expire/)).not.toBeInTheDocument();
    fireEvent.change(within(dialog).getByLabelText(/^Expires on/), { target: { value: "" } });
    await userEvent.click(within(dialog).getByRole("button", { name: "Verify" }));
    await waitFor(async () =>
      expect(
        await bodyOf(fetchMock, "POST", `${V}/qualification-records/${RECORD}/verify`),
      ).toEqual({ acquired_on: "2026-01-02" }),
    );
  });

  it("recording: the end date follows the qualification's validity until the person types one", async () => {
    mount({
      path: `/volunteers/people/${OTHER}`,
      perms: [`volunteers.qualifications.verify@${DEPT}`],
      routes: { [`GET ${V}/people/${OTHER}/qualifications`]: () => json({}) },
    });
    await screen.findByRole("option", { name: "First aid" });
    const qual = screen.getByLabelText(/^Qualification/);
    const acquired = screen.getByLabelText(/^Acquired on/);
    const expires = screen.getByLabelText(/^Expires on/);
    await userEvent.selectOptions(qual, QUAL);
    fireEvent.change(acquired, { target: { value: "2026-03-01" } });
    expect(expires).toHaveValue("2027-03-01");
    expect(screen.getByText(/usual validity of 365 days/)).toBeInTheDocument();
    fireEvent.change(expires, { target: { value: "2026-12-31" } });
    fireEvent.change(acquired, { target: { value: "2026-04-01" } });
    expect(expires).toHaveValue("2026-12-31");
    // a qualification without validity: nothing to fill in, and the hint says so
    await userEvent.selectOptions(qual, QUAL2);
    expect(screen.queryByText(/usual validity of/)).not.toBeInTheDocument();
    expect(screen.getByText(/usual validity, if it has one/)).toBeInTheDocument();
  });

  it("recording with nothing filled sends no empty strings, so the service names the fields", async () => {
    const { fetchMock } = mount({
      path: `/volunteers/people/${OTHER}`,
      perms: [`volunteers.qualifications.verify@${DEPT}`],
      routes: {
        [`GET ${V}/people/${OTHER}/qualifications`]: () => json({}),
        [`POST ${V}/people/${OTHER}/qualifications`]: () =>
          invalid(
            { field: "qualification", code: "required" },
            { field: "acquired_on", code: "required" },
          ),
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: "Record qualification" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Qualification: Required.");
    expect(screen.getByLabelText(/^Qualification/)).toHaveAttribute("aria-invalid", "true");
    expect(await bodyOf(fetchMock, "POST", `${V}/people/${OTHER}/qualifications`)).toEqual({});
  });

  it("addDays handles month, year and leap-day ends", () => {
    expect(addDays("2026-03-01", 365)).toBe("2027-03-01");
    expect(addDays("2027-12-31", 1)).toBe("2028-01-01");
    expect(addDays("2027-02-28", 1)).toBe("2027-03-01");
    expect(addDays("nope", 1)).toBe("");
  });
});

describe("volunteers: requirements never save from an unloaded catalogue", () => {
  it("a failed qualification read shows the error with retry and Save stays disabled", async () => {
    let fail = true;
    const { fetchMock } = mount({
      path: `${dpath}?tab=positions`,
      perms: LEAD,
      routes: {
        [`GET ${V}/qualifications`]: () =>
          fail
            ? problem(500, { code: "internal" })
            : json({ items: [{ id: QUAL, name: "First aid" }] }),
        [`PUT ${V}/positions/${POS}/requirements`]: () => json({ id: POS }),
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: /^Requirements/ }));
    const dialog = await screen.findByRole("dialog", { name: "Requirements for Greeter" });
    expect(await within(dialog).findByRole("alert")).toBeInTheDocument();
    const save = within(dialog).getByRole("button", { name: "Save requirements" });
    expect(save).toBeDisabled();
    fail = false;
    await userEvent.click(within(dialog).getByRole("button", { name: "Try again" }));
    await within(dialog).findByLabelText("First aid");
    await waitFor(() =>
      expect(within(dialog).getByRole("button", { name: "Save requirements" })).toBeEnabled(),
    );
    expect(seen(fetchMock, "PUT", `${V}/positions/${POS}/requirements`)).toBe(false);
  });
});

describe("volunteers: empty required fields are left out of requests", () => {
  it("the shift form sends no empty position, name, times or places", async () => {
    const { fetchMock } = mount({
      path: `${dpath}/shifts/new`,
      perms: LEAD,
      routes: {
        [`POST ${V}/departments/${DEPT}/shifts`]: () =>
          invalid(
            { field: "position", code: "required" },
            { field: "label", code: "required" },
            { field: "starts", code: "required" },
          ),
      },
    });
    await screen.findByRole("option", { name: "Greeter" });
    await userEvent.clear(screen.getByLabelText(/^Places/));
    await userEvent.click(screen.getByRole("button", { name: "Create shift" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Name of the shift: Required.");
    expect(screen.getByLabelText(/^Starts/)).toHaveAttribute("aria-invalid", "true");
    const body = (await bodyOf(fetchMock, "POST", `${V}/departments/${DEPT}/shifts`)) as Record<
      string,
      unknown
    >;
    for (const k of ["position", "label", "starts", "ends", "slots"])
      expect(body).not.toHaveProperty(k);
  });

  it("copy sends no empty event", async () => {
    const { fetchMock } = mount({
      path: `${dpath}?tab=shifts`,
      perms: LEAD,
      routes: {
        [`GET ${V}/shifts`]: () => json({}),
        [`POST ${V}/departments/${DEPT}/shifts/copy`]: () =>
          invalid({ field: "from", code: "required" }, { field: "to", code: "required" }),
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: /^Copy from/ }));
    const dialog = await screen.findByRole("dialog");
    await userEvent.click(within(dialog).getByRole("button", { name: /^Copy shifts/ }));
    expect(await within(dialog).findByRole("alert")).toBeInTheDocument();
    expect(await bodyOf(fetchMock, "POST", `${V}/departments/${DEPT}/shifts/copy`)).toEqual({});
  });
});

describe("volunteers: field error wording", () => {
  it("a limit with min and max reads 'between'; date order has its own wording", async () => {
    mount({
      path: `${dpath}/shifts/new`,
      perms: LEAD,
      routes: {
        [`POST ${V}/departments/${DEPT}/shifts`]: () =>
          invalid(
            { field: "slots", code: "range", params: { min: "1", max: "200" } },
            { field: "ends", code: "range" },
          ),
      },
    });
    await screen.findByRole("option", { name: "Greeter" });
    await userEvent.click(screen.getByRole("button", { name: "Create shift" }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Places: Must be between 1 and 200.");
    expect(alert).toHaveTextContent("Check the dates");
  });

  it("withRangeCodes renames only a range without min and max", () => {
    const err = (fields: ApiError["fields"]) =>
      new ApiError({
        status: 400,
        code: "invalid",
        fields,
        method: "POST",
        route: "/x",
        module: "volunteers",
        traceId: "t",
      });
    const out = withRangeCodes(
      err([
        { field: "a", code: "range", params: { min: "1", max: "2" }, reason: "" },
        { field: "b", code: "range", reason: "" },
      ]),
    );
    expect(out.fields.map((f) => f.code)).toEqual(["range", "range_order"]);
    expect(out).toBeInstanceOf(ApiError);
  });
});

describe("volunteers: the shift form's reads", () => {
  it("a failed positions read shows the error with retry, not an empty form", async () => {
    let fail = true;
    mount({
      path: `${dpath}/shifts/new`,
      perms: LEAD,
      routes: {
        [`GET ${V}/departments/${DEPT}/positions`]: () =>
          fail
            ? problem(500, { code: "internal" })
            : json({ items: [{ id: POS, department: DEPT, name: "Greeter", access: "open" }] }),
      },
    });
    expect(await screen.findByRole("alert")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Create shift" })).not.toBeInTheDocument();
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("button", { name: "Create shift" })).toBeInTheDocument();
  });

  it("a failed events read shows the error too", async () => {
    mount({
      path: `${dpath}/shifts/new`,
      perms: LEAD,
      routes: { "GET /api/events/": () => problem(500, { code: "internal" }) },
    });
    expect(await screen.findByRole("alert")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Create shift" })).not.toBeInTheDocument();
  });
});

describe("volunteers: focus after the opener is gone", () => {
  it("a lead's tab: rejecting a certificate moves focus to the tab heading", async () => {
    let items = [pendingRec];
    mount({
      path: `${dpath}?tab=pending`,
      perms: LEAD,
      routes: {
        [`GET ${V}/qualification-records`]: () => json({ items }),
        [`POST ${V}/qualification-records/${RECORD}/reject`]: () => {
          items = [];
          return none();
        },
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: /^Reject/ }));
    const dialog = await screen.findByRole("dialog");
    await userEvent.click(within(dialog).getByRole("button", { name: "Reject certificate" }));
    await waitFor(() =>
      expect(screen.getByRole("heading", { level: 2, name: "Pending certificates" })).toHaveFocus(),
    );
  });

  it("verifying moves focus to the tab heading", async () => {
    let items = [pendingRec];
    mount({
      path: `${dpath}?tab=pending`,
      perms: LEAD,
      routes: {
        [`GET ${V}/qualification-records`]: () => json({ items }),
        [`POST ${V}/qualification-records/${RECORD}/verify`]: () => {
          items = [];
          return json({ id: RECORD });
        },
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: /^Verify/ }));
    const dialog = await screen.findByRole("dialog");
    await userEvent.click(within(dialog).getByRole("button", { name: "Verify" }));
    await waitFor(() =>
      expect(screen.getByRole("heading", { level: 2, name: "Pending certificates" })).toHaveFocus(),
    );
  });

  it("my shifts, my departments, my certificates: focus goes to the section heading", async () => {
    let schedule = [
      { id: SIGNUP, status: "booked", shift: shift(), department_name: "Gate", position_name: "G" },
    ];
    let depts = [{ id: "m1", department: DEPT, person: ME }];
    let recs = [{ id: RECORD, qualification: QUAL, person: ME, acquired_on: "2026-01-02" }];
    mount({
      path: "/volunteers/me",
      routes: {
        [`GET ${V}/me/schedule`]: () => json({ items: schedule }),
        [`GET ${V}/me/departments`]: () => json({ items: depts }),
        [`GET ${V}/people/${ME}/qualifications`]: () => json({ items: recs }),
        [`GET ${V}/me/listing`]: () => json({ listed: true }),
        [`DELETE ${V}/signups/${SIGNUP}`]: () => {
          schedule = [];
          return none();
        },
        [`DELETE ${V}/me/departments/${DEPT}`]: () => {
          depts = [];
          return none();
        },
        [`DELETE ${V}/qualification-records/${RECORD}`]: () => {
          recs = [];
          return none();
        },
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: /^Cancel booking/ }));
    await userEvent.click(
      within(await screen.findByRole("dialog")).getByRole("button", { name: "Cancel my booking" }),
    );
    await waitFor(() =>
      expect(screen.getByRole("heading", { level: 2, name: "My shifts" })).toHaveFocus(),
    );
    await userEvent.click(await screen.findByRole("button", { name: /^Leave/ }));
    await userEvent.click(
      within(await screen.findByRole("dialog")).getByRole("button", { name: "Leave department" }),
    );
    await waitFor(() => expect(document.activeElement?.id).toBe("me-departments"));
    await userEvent.click(await screen.findByRole("button", { name: /^Withdraw/ }));
    await userEvent.click(
      within(await screen.findByRole("dialog")).getByRole("button", {
        name: "Withdraw certificate",
      }),
    );
    await waitFor(() => expect(document.activeElement?.id).toBe("me-quals"));
  });

  it("removing a member moves focus to the tab heading", async () => {
    let members = [{ id: "m1", department: DEPT, person: OTHER, playa_name: "Pat Dust" }];
    mount({
      path: `${dpath}?tab=members`,
      perms: LEAD,
      routes: {
        [`GET ${V}/departments/${DEPT}/members`]: () => json({ items: members }),
        [`DELETE ${V}/departments/${DEPT}/members/${OTHER}`]: () => {
          members = [];
          return none();
        },
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: /^Remove/ }));
    await userEvent.click(
      within(await screen.findByRole("dialog")).getByRole("button", { name: "Remove member" }),
    );
    await waitFor(() => expect(document.activeElement?.id).toBe("tab-heading"));
  });

  it("signing up moves focus to the new 'You're booked' status", async () => {
    let booked = false;
    mount({
      path: "/volunteers",
      routes: {
        [`GET ${V}/shifts`]: () =>
          json({
            items: [
              shift(booked ? { booked: 1, mine: SIGNUP } : { eligibility: { eligible: true } }),
            ],
          }),
        [`POST ${V}/shifts/${SHIFT}/signups`]: () => {
          booked = true;
          return json({ id: SIGNUP, shift: SHIFT }, { status: 201 });
        },
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: /^Sign up/ }));
    const status = await screen.findByText("You're booked");
    await waitFor(() => expect(status).toHaveFocus());
  });
});

describe("volunteers: Shifts list", () => {
  it("the day filter is sent to the server as a window around the day, then trimmed exactly", async () => {
    const { fetchMock } = mount({
      path: "/volunteers?day=2099-07-02",
      routes: {
        [`GET ${V}/shifts`]: () =>
          json({
            items: [
              shift({ id: "a", label: "Day one", starts: "2099-07-01T15:00:00Z" }),
              shift({ id: "b", label: "Day two", starts: "2099-07-02T15:00:00Z" }),
            ],
          }),
      },
    });
    expect(await screen.findByRole("heading", { level: 3, name: "Day two" })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { level: 3, name: "Day one" })).not.toBeInTheDocument();
    const q = queryOf(fetchMock, "GET", `${V}/shifts`);
    expect(q?.get("from")).toBeTruthy();
    expect(q?.get("to")).toBeTruthy();
    expect(Date.parse(q!.get("from")!)).toBeLessThan(Date.parse("2099-07-02T00:00:00Z"));
    expect(Date.parse(q!.get("to")!)).toBeGreaterThan(Date.parse("2099-07-03T00:00:00Z"));
  });

  it("Show more appends the next page with the cursor, and goes away on the last page", async () => {
    const { fetchMock } = mount({
      path: "/volunteers",
      routes: {
        [`GET ${V}/shifts`]: (req) => {
          const after = new URL(req.url).searchParams.get("after");
          return after === "c1"
            ? json({ items: [shift({ id: "s2", label: "Second page" })] })
            : json({ items: [shift({ id: "s1", label: "First page" })], next: "c1" });
        },
      },
    });
    await screen.findByRole("heading", { level: 3, name: "First page" });
    expect(queryOf(fetchMock, "GET", `${V}/shifts`)?.get("limit")).toBe("100");
    await userEvent.click(screen.getByRole("button", { name: "Show more" }));
    await screen.findByRole("heading", { level: 3, name: "Second page" });
    expect(screen.getByRole("heading", { level: 3, name: "First page" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Show more" })).not.toBeInTheDocument();
  });

  it("a failed next page says so in place and keeps the rows", async () => {
    mount({
      path: "/volunteers",
      routes: {
        [`GET ${V}/shifts`]: (req) =>
          new URL(req.url).searchParams.get("after")
            ? problem(500, { code: "internal", ref: "ABCD-1234" })
            : json({ items: [shift({ label: "First page" })], next: "c1" }),
      },
    });
    await screen.findByRole("heading", { level: 3, name: "First page" });
    await userEvent.click(screen.getByRole("button", { name: "Show more" }));
    expect(await screen.findByRole("alert")).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 3, name: "First page" })).toBeInTheDocument();
  });

  it("phones get a Filters button that folds the filters away; it starts open when some are set", async () => {
    const { unmount } = mount({
      path: "/volunteers",
      routes: { [`GET ${V}/shifts`]: () => json({}) },
    });
    const toggle = await screen.findByRole("button", { name: "Filters" });
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    await userEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-expanded", "true");
    unmount();
    mount({
      path: "/volunteers?department=" + DEPT,
      routes: { [`GET ${V}/shifts`]: () => json({}) },
    });
    expect(await screen.findByRole("button", { name: "Filters" })).toHaveAttribute(
      "aria-expanded",
      "true",
    );
  });

  it("a refused sign-up refreshes the card", async () => {
    let reads = 0;
    mount({
      path: "/volunteers",
      routes: {
        [`GET ${V}/shifts`]: () => {
          reads += 1;
          return json({ items: [shift({ eligibility: { eligible: true } })] });
        },
        [`POST ${V}/shifts/${SHIFT}/signups`]: () =>
          problem(409, {
            code: "conflict",
            fields: [{ field: "shift", code: "full", reason: "" }],
          }),
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: /^Sign up/ }));
    await screen.findByText("This shift is full.");
    await waitFor(() => expect(reads).toBeGreaterThan(1));
  });
});

describe("volunteers: a shift page", () => {
  const path = `/volunteers/shifts/${SHIFT}`;

  it("has its h1 while loading and when the shift is not found", async () => {
    mount({
      path,
      routes: { [`GET ${V}/shifts/${SHIFT}`]: () => problem(404, { code: "not_found" }) },
    });
    expect(await screen.findByRole("heading", { level: 1, name: "Shift" })).toBeInTheDocument();
    await screen.findByRole("alert");
    expect(screen.getByRole("heading", { level: 1, name: "Shift" })).toBeInTheDocument();
  });

  it("a canceled shift offers no Edit; a live one does", async () => {
    const { unmount } = mount({
      path,
      perms: ["volunteers.shifts.manage", "volunteers.rosters.read"],
      routes: {
        [`GET ${V}/shifts/${SHIFT}`]: () => json(shift({ canceled_at: "2099-01-01T00:00:00Z" })),
        [`GET ${V}/shifts/${SHIFT}/roster`]: () => json({ shift: shift() }),
      },
    });
    await screen.findByText(/This shift was canceled\./);
    expect(screen.queryByRole("link", { name: "Edit shift" })).not.toBeInTheDocument();
    unmount();
    mount({
      path,
      perms: ["volunteers.shifts.manage", "volunteers.rosters.read"],
      routes: {
        [`GET ${V}/shifts/${SHIFT}`]: () => json(shift()),
        [`GET ${V}/shifts/${SHIFT}/roster`]: () => json({ shift: shift() }),
      },
    });
    expect(await screen.findByRole("link", { name: "Edit shift" })).toBeInTheDocument();
  });

  it("Manage (with Assign) shows without rosters.read and the roster shows without Manage", async () => {
    const manager = mount({
      path,
      perms: ["volunteers.shifts.manage"],
      routes: { [`GET ${V}/shifts/${SHIFT}`]: () => json(shift()) },
    });
    expect(await screen.findByRole("heading", { name: "Manage this shift" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Assign someone" })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Roster" })).not.toBeInTheDocument();
    manager.unmount();
    mount({
      path,
      perms: ["volunteers.rosters.read"],
      routes: {
        [`GET ${V}/shifts/${SHIFT}`]: () => json(shift()),
        [`GET ${V}/shifts/${SHIFT}/roster`]: () => json({ shift: shift(), entries: [] }),
      },
    });
    expect(await screen.findByRole("heading", { name: "Roster" })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Manage this shift" })).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Assign someone" })).not.toBeInTheDocument();
  });

  it("a person picked without a playa name is shown as 'No playa name yet', never an ID", async () => {
    mount({
      path,
      perms: ["volunteers.shifts.manage", "volunteers.rosters.read"],
      routes: {
        [`GET ${V}/shifts/${SHIFT}`]: () => json(shift()),
        [`GET ${V}/shifts/${SHIFT}/roster`]: () => json({ shift: shift() }),
        "GET /api/people/search": () =>
          json({ matches: [{ id: "7d2c2f0e-0000-4000-8000-0000000000aa", pronouns: "" }] }),
      },
    });
    await userEvent.type(await screen.findByLabelText("Playa name"), "xx");
    await userEvent.click(screen.getByRole("button", { name: "Search" }));
    await userEvent.click(await screen.findByRole("button", { name: /No playa name yet/ }));
    expect(screen.queryByText(/7d2c2f0e/)).not.toBeInTheDocument();
  });
});
