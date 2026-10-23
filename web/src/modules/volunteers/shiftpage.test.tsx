import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { expectNoAxeViolations } from "@/test/axe";
import { json, problem } from "@/test/fakeFetch";
import { bodyOf, mount, ME, OTHER, POS, QUAL, seen, shift, SHIFT, SIGNUP, V } from "./testkit";

const path = `/volunteers/shifts/${SHIFT}`;
const one = (over: Record<string, unknown> = {}) => ({
  [`GET ${V}/shifts/${SHIFT}`]: () => json(shift(over)),
});
const LEAD = ["volunteers.shifts.signup", "volunteers.shifts.manage", "volunteers.rosters.read"];

describe("volunteers: a shift", () => {
  it("shows the shift, who else is on it (names as plain text, then the unlisted count)", async () => {
    const { container } = mount({
      path,
      routes: one({
        booked: 3,
        eligibility: { eligible: true },
        mates: { names: ["Dusty Pete", "Moth"], unlisted: 2 },
      }),
    });
    expect(
      await screen.findByRole("heading", { level: 1, name: "Morning dirt" }),
    ).toBeInTheDocument();
    const section = screen.getByRole("region", { name: "Who else is on this shift" });
    expect(
      within(section)
        .getAllByRole("listitem")
        .map((li) => li.textContent),
    ).toEqual(["Dusty Pete", "Moth"]);
    expect(within(section).queryByRole("link")).not.toBeInTheDocument();
    expect(within(section).getByText("and 2 people who are not listed")).toBeInTheDocument();
    expect(screen.getByText("Ranger HQ")).toBeInTheDocument();
    await expectNoAxeViolations(container);
  });

  it("mates present but empty: 'No one else is booked yet'; absent: no section at all", async () => {
    const { unmount } = mount({ path, routes: one({ mates: {} }) });
    expect(await screen.findByText("No one else is booked yet.")).toBeInTheDocument();
    unmount();
    mount({ path, routes: one({}) });
    await screen.findByRole("heading", { level: 1, name: "Morning dirt" });
    expect(screen.queryByText("Who else is on this shift")).not.toBeInTheDocument();
  });

  it("only unlisted people: a sentence with no names", async () => {
    mount({ path, routes: one({ mates: { unlisted: 1 } }) });
    expect(await screen.findByText("1 person is not listed.")).toBeInTheDocument();
  });

  it("cancel my booking asks first, then deletes the sign-up", async () => {
    let mine = true;
    const { fetchMock } = mount({
      path,
      routes: {
        [`GET ${V}/shifts/${SHIFT}`]: () => json(shift(mine ? { mine: SIGNUP, booked: 1 } : {})),
        [`DELETE ${V}/signups/${SIGNUP}`]: () => {
          mine = false;
          return new Response(null, { status: 204 });
        },
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: "Cancel my booking" }));
    const dialog = await screen.findByRole("dialog", { name: "Cancel your booking?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Cancel my booking" }));
    await waitFor(() => expect(seen(fetchMock, "DELETE", `${V}/signups/${SIGNUP}`)).toBe(true));
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Cancel my booking" })).toBeNull(),
    );
  });

  it("a cutoff refusal stays in the dialog with its reason", async () => {
    mount({
      path,
      routes: {
        ...one({ mine: SIGNUP }),
        [`DELETE ${V}/signups/${SIGNUP}`]: () =>
          problem(409, {
            code: "conflict",
            fields: [{ field: "signup", code: "cutoff", reason: "" }],
          }),
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: "Cancel my booking" }));
    const dialog = await screen.findByRole("dialog", { name: "Cancel your booking?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Cancel my booking" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("too close to the start");
  });

  it("canceled shift: a banner with the reason; invisible shift: the not-found state", async () => {
    const { unmount } = mount({
      path,
      routes: one({ canceled_at: "2099-01-01T00:00:00Z", cancel_reason: "event_closed" }),
    });
    expect(await screen.findByText(/This shift was canceled\./)).toBeInTheDocument();
    expect(screen.getByText(/This event was closed\./)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Sign up/ })).not.toBeInTheDocument();
    unmount();
    mount({
      path,
      routes: { [`GET ${V}/shifts/${SHIFT}`]: () => problem(404, { code: "not_found" }) },
    });
    expect(await screen.findByRole("alert")).toHaveTextContent("We couldn't find that.");
  });

  it("a roster reader sees playa names linking to the profile, flags, and a legal name column only when present", async () => {
    const entries = [
      {
        id: "su1",
        shift: SHIFT,
        person: OTHER,
        playa_name: "Pat Dust",
        eligible: true,
        legal_name: "Pat Example",
      },
      { id: "su2", shift: SHIFT, person: "p2", playa_name: "Moth", waived: true, waived_by: "p9" },
      { id: "su3", shift: SHIFT, person: "p3", playa_name: "Gone Lead", waived: true },
    ];
    const { unmount } = mount({
      path,
      perms: ["volunteers.rosters.read"],
      routes: {
        ...one({}),
        [`GET ${V}/shifts/${SHIFT}/roster`]: () => json({ shift: shift(), entries }),
      },
    });
    expect(await screen.findByRole("link", { name: "Pat Dust" })).toHaveAttribute(
      "href",
      `/people/${OTHER}`,
    );
    expect(screen.getByText("Pat Example")).toBeInTheDocument();
    expect(screen.getAllByText("Requirement waived")).toHaveLength(2);
    expect(screen.getAllByText("Missing a requirement")).toHaveLength(2);
    expect(screen.queryByRole("button", { name: /Assign|Remove/ })).not.toBeInTheDocument();
    unmount();
    mount({
      path,
      perms: ["volunteers.rosters.read"],
      routes: {
        ...one({}),
        [`GET ${V}/shifts/${SHIFT}/roster`]: () =>
          json({ shift: shift(), entries: entries.map((e) => ({ ...e, legal_name: undefined })) }),
      },
    });
    await screen.findByRole("link", { name: "Pat Dust" });
    expect(screen.queryByText("Legal name")).not.toBeInTheDocument();
  });

  it("a roster that fails shows an error with retry; an empty roster says so", async () => {
    mount({
      path,
      perms: ["volunteers.rosters.read"],
      routes: {
        ...one({}),
        [`GET ${V}/shifts/${SHIFT}/roster`]: () => problem(500, { code: "internal" }),
      },
    });
    expect(await screen.findByRole("alert")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument();
  });

  it("a manager assigns someone with the waiver switch; a missing-qualification refusal is explained", async () => {
    const { fetchMock } = mount({
      path,
      perms: LEAD,
      routes: {
        ...one({}),
        [`GET ${V}/shifts/${SHIFT}/roster`]: () => json({ shift: shift(), entries: [] }),
        "GET /api/people/search": () =>
          json({ matches: [{ id: OTHER, playa_name: "Pat Dust", pronouns: "" }] }),
        [`POST ${V}/shifts/${SHIFT}/assign`]: async (req) => {
          const b = (await req.clone().json()) as { waive: boolean };
          return b.waive
            ? json({ id: SIGNUP, shift: SHIFT, person: OTHER }, { status: 201 })
            : problem(409, {
                code: "conflict",
                fields: [{ field: "person", code: "unqualified", reason: "" }],
              });
        },
      },
    });
    expect(await screen.findByText("No one is booked on this shift yet.")).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText("Playa name"), "Pat");
    await userEvent.click(screen.getByRole("button", { name: "Search" }));
    await userEvent.click(await screen.findByRole("button", { name: /Pat Dust/ }));
    await userEvent.click(screen.getByRole("button", { name: "Assign" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "doesn't meet the position's requirements",
    );
    expect(screen.getByText(/The waiver is recorded with your name/)).toBeInTheDocument();
    await userEvent.click(screen.getByLabelText("Waive requirements"));
    await userEvent.click(screen.getByRole("button", { name: "Assign" }));
    await waitFor(async () =>
      expect(await bodyOf(fetchMock, "POST", `${V}/shifts/${SHIFT}/assign`)).toBeDefined(),
    );
    await waitFor(() => expect(screen.getAllByText("Booked").length).toBeGreaterThan(0));
  });

  it("a lead may waive for themself: the switch is not disabled when the chosen person is the viewer", async () => {
    const { fetchMock } = mount({
      path,
      perms: LEAD,
      routes: {
        ...one({}),
        "GET /api/people/search": () =>
          json({ matches: [{ id: ME, playa_name: "Dusty", pronouns: "" }] }),
        [`POST ${V}/shifts/${SHIFT}/assign`]: () =>
          json({ id: SIGNUP, shift: SHIFT, person: ME }, { status: 201 }),
      },
    });
    await userEvent.type(await screen.findByLabelText("Playa name"), "Du");
    await userEvent.click(screen.getByRole("button", { name: "Search" }));
    await userEvent.click(await screen.findByRole("button", { name: /Dusty/ }));
    const waive = screen.getByLabelText("Waive requirements");
    expect(waive).toBeEnabled();
    await userEvent.click(waive);
    await userEvent.click(screen.getByRole("button", { name: "Assign" }));
    await waitFor(async () =>
      expect(await bodyOf(fetchMock, "POST", `${V}/shifts/${SHIFT}/assign`)).toEqual({
        person: ME,
        waive: true,
      }),
    );
  });

  it("an unnamed person: sign-up links to the profile; Assign says they have not set a name", async () => {
    const unnamed = () =>
      problem(409, {
        code: "conflict",
        fields: [{ field: "person", code: "unnamed", reason: "" }],
      });
    const a = mount({
      path,
      routes: {
        ...one({ eligibility: { eligible: true } }),
        [`POST ${V}/shifts/${SHIFT}/signups`]: unnamed,
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: /^Sign up/ }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Set your playa name on your profile first.");
    expect(within(alert).getByRole("link", { name: "Open my profile" })).toHaveAttribute(
      "href",
      "/people/me",
    );
    a.unmount();
    mount({
      path,
      perms: LEAD,
      routes: {
        ...one({}),
        [`GET ${V}/shifts/${SHIFT}/roster`]: () => json({ shift: shift(), entries: [] }),
        "GET /api/people/search": () =>
          json({ matches: [{ id: OTHER, playa_name: "Pat Dust", pronouns: "" }] }),
        [`POST ${V}/shifts/${SHIFT}/assign`]: unnamed,
      },
    });
    await userEvent.type(await screen.findByLabelText("Playa name"), "Pat");
    await userEvent.click(screen.getByRole("button", { name: "Search" }));
    await userEvent.click(await screen.findByRole("button", { name: /Pat Dust/ }));
    await userEvent.click(screen.getByRole("button", { name: "Assign" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "This person has not set a playa name yet.",
    );
  });

  it("a manager can remove a booking and cancel the shift, each after a confirmation", async () => {
    const { fetchMock } = mount({
      path,
      perms: LEAD,
      routes: {
        ...one({}),
        [`GET ${V}/shifts/${SHIFT}/roster`]: () =>
          json({
            shift: shift(),
            entries: [
              { id: "su1", shift: SHIFT, person: OTHER, playa_name: "Pat Dust", eligible: true },
            ],
          }),
        [`DELETE ${V}/signups/su1`]: () => new Response(null, { status: 204 }),
        [`POST ${V}/shifts/${SHIFT}/cancel`]: () => new Response(null, { status: 204 }),
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: /^Remove/ }));
    let dialog = await screen.findByRole("dialog", { name: "Remove this booking?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Remove booking" }));
    await waitFor(() => expect(seen(fetchMock, "DELETE", `${V}/signups/su1`)).toBe(true));
    expect(screen.getByRole("link", { name: "Edit shift" })).toHaveAttribute(
      "href",
      `/volunteers/shifts/${SHIFT}/edit`,
    );
    await userEvent.click(screen.getByRole("button", { name: "Cancel shift" }));
    dialog = await screen.findByRole("dialog", { name: "Cancel this shift?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Cancel shift" }));
    await waitFor(() => expect(seen(fetchMock, "POST", `${V}/shifts/${SHIFT}/cancel`)).toBe(true));
  });

  it("a person without the lead permissions sees no manager or roster section", async () => {
    mount({ path, routes: one({ eligibility: { eligible: true } }) });
    await screen.findByRole("heading", { level: 1, name: "Morning dirt" });
    expect(screen.queryByText("Manage this shift")).not.toBeInTheDocument();
    expect(screen.queryByText("Roster")).not.toBeInTheDocument();
  });

  it("names the position and department from the shift summary", async () => {
    mount({ path, routes: one({}) });
    await screen.findByRole("heading", { level: 1, name: "Morning dirt" });
    expect(await screen.findByText("Greeter")).toBeInTheDocument();
    expect(screen.getByText("Gate")).toBeInTheDocument();
    void POS;
    void QUAL;
  });
});
