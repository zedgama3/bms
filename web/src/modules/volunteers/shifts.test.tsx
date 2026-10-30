import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { expectNoAxeViolations } from "@/test/axe";
import { json, problem } from "@/test/fakeFetch";
import { DEPT, EVENT, mount, queryOf, QUAL, seen, shift, SHIFT, SIGNUP, V } from "./testkit";

const list =
  (...items: unknown[]) =>
  () =>
    json({ items });

const publicDoc = (terms: Record<string, string>) => ({
  name: "Dust Bowl",
  logo: "",
  contact_email: "",
  language: "en",
  languages: ["en"],
  public_url: "",
  theme: false,
  timezone: "",
  terms: { en: terms },
});

describe("volunteers: the regional's word for shift (F170)", () => {
  it("renders the regional's word for shift", async () => {
    mount({
      path: "/volunteers",
      routes: {
        "GET /api/settings/public": () =>
          json(
            publicDoc({
              term_shift: "stint",
              term_shift_plural: "stints",
              term_shift_title: "Stint",
              term_shift_plural_title: "Stints",
            }),
          ),
        [`GET ${V}/shifts`]: list(),
      },
    });
    expect(await screen.findByRole("heading", { level: 1, name: "Stints" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Stints" })).toBeInTheDocument();
    expect(await screen.findByText("No stints to show yet.")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Shifts" })).not.toBeInTheDocument();
  });

  it("falls back to the module's default when there is no document", async () => {
    mount({ path: "/volunteers", routes: { [`GET ${V}/shifts`]: list() } });
    expect(await screen.findByRole("heading", { level: 1, name: "Shifts" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Shifts" })).toBeInTheDocument();
    expect(await screen.findByText("No shifts to show yet.")).toBeInTheDocument();
  });
});

describe("volunteers: Shifts", () => {
  it("lists shifts with fill and Sign up; the nav has the two main entries", async () => {
    const { container } = mount({
      path: "/volunteers",
      routes: {
        [`GET ${V}/shifts`]: list(shift({ booked: 1, eligibility: { eligible: true } })),
      },
    });
    expect(await screen.findByRole("heading", { level: 1, name: "Shifts" })).toBeInTheDocument();
    const card = (await screen.findByRole("heading", { level: 3, name: "Morning dirt" })).closest(
      "li",
    )!;
    expect(within(card).getByText("1 of 4 booked")).toBeInTheDocument();
    expect(within(card).getByText(/Gate · Greeter/)).toBeInTheDocument();
    expect(within(card).getByRole("button", { name: /^Sign up/ })).toBeInTheDocument();
    expect(screen.getAllByRole("link", { name: "My volunteering" }).length).toBeGreaterThan(0);
    await expectNoAxeViolations(container);
  });

  it("an absent booked count reads as zero", async () => {
    mount({
      path: "/volunteers",
      routes: { [`GET ${V}/shifts`]: list(shift({ eligibility: { eligible: true } })) },
    });
    expect(await screen.findByText("0 of 4 booked")).toBeInTheDocument();
  });

  it("sign up posts, then shows Booked", async () => {
    let booked = false;
    const { fetchMock } = mount({
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
    expect(await screen.findByText("You're booked")).toBeInTheDocument();
    expect(seen(fetchMock, "POST", `${V}/shifts/${SHIFT}/signups`)).toBe(true);
  });

  it("says why a person cannot sign up, naming the missing qualification", async () => {
    mount({
      path: "/volunteers",
      routes: {
        [`GET ${V}/shifts`]: list(
          shift({ id: "s1", label: "Gate shift", eligibility: { missing: [QUAL] } }),
          shift({ id: "s2", label: "Members shift", eligibility: { reason: "not_member" } }),
          shift({ id: "s3", label: "Full shift", booked: 4, eligibility: { reason: "full" } }),
          shift({ id: "s4", label: "Clash shift", eligibility: { reason: "overlap" } }),
        ),
      },
    });
    expect(await screen.findByText("Needs First aid")).toBeInTheDocument();
    expect(screen.getByText("Members only")).toBeInTheDocument();
    expect(screen.getByText("Full")).toBeInTheDocument();
    expect(screen.getByText("Overlaps a booking of yours")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Sign up/ })).not.toBeInTheDocument();
    // the shift page keeps the sentence
    expect(screen.queryByText("This shift is full.")).not.toBeInTheDocument();
  });

  it("rows show times under the day heading, say next day for a night shift, and one booked phrasing", async () => {
    mount({
      path: "/volunteers",
      routes: {
        [`GET ${V}/shifts`]: list(
          shift({
            id: "a",
            label: "Late one",
            starts: "2099-07-01T15:00:00Z",
            ends: "2099-07-01T19:00:00Z",
            location: "",
          }),
          shift({
            id: "b",
            label: "Night one",
            starts: "2099-07-02T03:00:00Z",
            ends: "2099-07-02T09:00:00Z",
            booked: 4,
          }),
        ),
      },
    });
    const late = (await screen.findByRole("heading", { level: 3, name: "Late one" })).closest(
      "li",
    )!;
    expect(within(late).getByText(/^9:00 AM.* to 1:00 PM/)).toBeInTheDocument();
    expect(within(late).queryByText(/2099/)).not.toBeInTheDocument();
    expect(within(late).getByText("Gate · Greeter")).toBeInTheDocument();
    const night = screen.getByRole("heading", { level: 3, name: "Night one" }).closest("li")!;
    expect(within(night).getByText(/to .* next day$/)).toBeInTheDocument();
    expect(within(night).getByText("4 of 4 booked")).toBeInTheDocument();
    expect(screen.getAllByRole("heading", { level: 2 }).length).toBeGreaterThanOrEqual(1);
  });

  it("the shift page keeps the full sentence beside the list's short reason", async () => {
    const full = shift({ booked: 4, eligibility: { reason: "full" } });
    const { unmount } = mount({ path: "/volunteers", routes: { [`GET ${V}/shifts`]: list(full) } });
    expect(await screen.findByText("Full")).toBeInTheDocument();
    expect(screen.queryByText("This shift is full.")).not.toBeInTheDocument();
    unmount();
    mount({
      path: `/volunteers/shifts/${SHIFT}`,
      routes: { [`GET ${V}/shifts/${SHIFT}`]: () => json(full) },
    });
    expect(await screen.findByText("This shift is full.")).toBeInTheDocument();
  });

  it("opening a shift from a filtered list returns to the same filters", async () => {
    const { router } = mount({
      path: "/volunteers?eligible=1",
      routes: {
        [`GET ${V}/shifts`]: list(shift({ eligibility: { eligible: true } })),
        [`GET ${V}/shifts/${SHIFT}`]: () => json(shift({ eligibility: { eligible: true } })),
      },
    });
    await userEvent.click(await screen.findByRole("link", { name: "Morning dirt" }));
    const back = await screen.findByRole("link", { name: "Back to shifts" });
    expect(back).toHaveAttribute("href", "/volunteers?eligible=1");
    expect(router()!.state.location.pathname).toBe(`/volunteers/shifts/${SHIFT}`);
  });

  it("Only shifts I can take sends eligible=true, keeps it in the URL and clears with the filters", async () => {
    const { fetchMock, router } = mount({
      path: "/volunteers",
      routes: { [`GET ${V}/shifts`]: list(shift({ eligibility: { eligible: true } })) },
    });
    await screen.findByRole("heading", { level: 3, name: "Morning dirt" });
    expect(queryOf(fetchMock, "GET", `${V}/shifts`)?.get("eligible")).toBeNull();
    await userEvent.click(screen.getByLabelText("Only shifts I can take"));
    await waitFor(() =>
      expect(queryOf(fetchMock, "GET", `${V}/shifts`)?.get("eligible")).toBe("true"),
    );
    expect(router()!.state.location.search).toBe("?eligible=1");
    await userEvent.click(screen.getAllByRole("button", { name: "Clear filters" })[0]!);
    await waitFor(() => expect(router()!.state.location.search).toBe(""));
    expect(screen.getByLabelText("Only shifts I can take")).not.toBeChecked();
  });

  it("the filter form starts open when eligible is set", async () => {
    mount({ path: "/volunteers?eligible=1", routes: { [`GET ${V}/shifts`]: list() } });
    expect(await screen.findByLabelText("Only shifts I can take")).toBeChecked();
    expect(screen.getByRole("button", { name: "Filters" })).toHaveAttribute(
      "aria-expanded",
      "true",
    );
  });

  it("an empty page with next shows Show more, not the empty state", async () => {
    let calls = 0;
    mount({
      path: "/volunteers?eligible=1",
      routes: {
        [`GET ${V}/shifts`]: () => {
          calls++;
          return calls === 1 ? json({ items: [], next: "c1" }) : json({ items: [shift()] });
        },
      },
    });
    const more = await screen.findByRole("button", { name: "Show more" });
    expect(screen.queryByText("No shifts to show yet.")).not.toBeInTheDocument();
    await userEvent.click(more);
    expect(
      await screen.findByRole("heading", { level: 3, name: "Morning dirt" }),
    ).toBeInTheDocument();
  });

  it("filters by event and department through the query, and by day in the browser", async () => {
    const { fetchMock } = mount({
      path: "/volunteers",
      routes: {
        [`GET ${V}/shifts`]: list(
          shift({ id: "a", label: "Day one", starts: "2099-07-01T15:00:00Z" }),
          shift({ id: "b", label: "Day two", starts: "2099-07-02T15:00:00Z" }),
        ),
      },
    });
    await screen.findByRole("heading", { level: 3, name: "Day one" });
    await userEvent.selectOptions(screen.getByLabelText("Event"), EVENT);
    await waitFor(() => expect(queryOf(fetchMock, "GET", `${V}/shifts`)?.get("event")).toBe(EVENT));
    await userEvent.selectOptions(screen.getByLabelText("Department"), DEPT);
    await waitFor(() =>
      expect(queryOf(fetchMock, "GET", `${V}/shifts`)?.get("department")).toBe(DEPT),
    );
    await userEvent.type(screen.getByLabelText("Day"), "2099-07-02");
    expect(await screen.findByRole("heading", { level: 3, name: "Day two" })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { level: 3, name: "Day one" })).not.toBeInTheDocument();
  });

  it("empty: says so, and offers to clear the filters when some are set", async () => {
    mount({ path: "/volunteers?event=" + EVENT, routes: { [`GET ${V}/shifts`]: list() } });
    expect(await screen.findByText("No shifts to show yet.")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "Clear filters" }).length).toBeGreaterThan(0);
  });

  it("error: ErrorState with Try again that reloads", async () => {
    let fail = true;
    mount({
      path: "/volunteers",
      routes: {
        [`GET ${V}/shifts`]: () =>
          fail ? problem(500, { code: "internal", ref: "CAFE-0001" }) : json({ items: [shift()] }),
      },
    });
    expect(await screen.findByRole("alert")).toHaveTextContent("Something went wrong.");
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(
      await screen.findByRole("heading", { level: 3, name: "Morning dirt" }),
    ).toBeInTheDocument();
  });

  it("forbidden read: the shell's forbidden sentence", async () => {
    mount({
      path: "/volunteers",
      routes: { [`GET ${V}/shifts`]: () => problem(403, { code: "permission", ref: "CAFE-0002" }) },
    });
    expect(await screen.findByText("You don't have access to this screen.")).toBeInTheDocument();
  });

  it("a refused sign-up (full) shows the reason; a 403 shows the forbidden sentence", async () => {
    let code = "full";
    mount({
      path: "/volunteers",
      routes: {
        [`GET ${V}/shifts`]: list(shift({ eligibility: { eligible: true } })),
        [`POST ${V}/shifts/${SHIFT}/signups`]: () =>
          code === "full"
            ? problem(409, {
                code: "conflict",
                fields: [{ field: "shift", code: "full", reason: "" }],
              })
            : problem(403, { code: "permission" }),
      },
    });
    const button = await screen.findByRole("button", { name: /^Sign up/ });
    await userEvent.click(button);
    expect(await screen.findByRole("alert")).toHaveTextContent("This shift is full.");
    code = "perm";
    await userEvent.click(screen.getByRole("button", { name: /^Sign up/ }));
    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent("You don't have access to this screen."),
    );
  });

  it("read-only copy: Sign up is disabled and says why", async () => {
    mount({
      path: "/volunteers",
      readOnly: () => true,
      routes: { [`GET ${V}/shifts`]: list(shift({ eligibility: { eligible: true } })) },
    });
    const button = await screen.findByRole("button", { name: /^Sign up/ });
    await waitFor(() => expect(button).toBeDisabled());
    expect(screen.getAllByText(/This copy is read-only/).length).toBeGreaterThan(0);
  });
});
