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
    expect(within(card).getByText("3 places free of 4")).toBeInTheDocument();
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
    expect(await screen.findByText("4 places free of 4")).toBeInTheDocument();
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
    expect(await screen.findByText("You need this qualification: First aid.")).toBeInTheDocument();
    expect(screen.getByText("Only members of this department can sign up.")).toBeInTheDocument();
    expect(screen.getByText("This shift is full.")).toBeInTheDocument();
    expect(
      screen.getByText("You're already booked on another shift at this time."),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Sign up/ })).not.toBeInTheDocument();
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
