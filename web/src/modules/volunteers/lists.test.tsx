import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { json, problem } from "@/test/fakeFetch";
import { DEPT, mount, OTHER, QUAL, SHIFT, V } from "./testkit";

const ALL = [
  "shifts.manage",
  "positions.manage",
  "members.manage",
  "qualifications.manage",
  "qualifications.verify",
  "rosters.read",
].map((p) => `volunteers.${p}@${DEPT}`);

describe("volunteers: empty lists arrive as {} (D46)", () => {
  it("every list reads an absent items as empty, and shows its empty state", async () => {
    const empty = () => json({});
    const routes = {
      [`GET ${V}/shifts`]: empty,
      [`GET ${V}/qualifications`]: empty,
      [`GET ${V}/departments/${DEPT}/positions`]: empty,
      [`GET ${V}/departments/${DEPT}/members`]: empty,
      [`GET ${V}/qualification-records`]: empty,
      [`GET ${V}/me/schedule`]: empty,
      [`GET ${V}/me/departments`]: empty,
      [`GET ${V}/me/listing`]: empty,
      [`GET ${V}/people/${OTHER}/qualifications`]: empty,
    };
    const checks: [string, string[], string][] = [
      ["/volunteers", [], "No shifts to show yet."],
      ["/volunteers/me", [], "You have no shifts booked."],
      [
        `/volunteers/departments/${DEPT}?tab=positions`,
        ALL,
        "No positions yet. Create the first one.",
      ],
      [`/volunteers/departments/${DEPT}?tab=members`, ALL, "No members yet."],
      [`/volunteers/departments/${DEPT}?tab=pending`, ALL, "No certificates are waiting."],
      [
        `/volunteers/departments/${DEPT}?tab=qualifications`,
        ALL,
        "No qualifications yet. Create the first one.",
      ],
      [`/volunteers/people/${OTHER}`, ALL, "No qualifications on record in your area."],
    ];
    for (const [path, perms, text] of checks) {
      const { unmount } = mount({ path, perms: perms.length ? perms : undefined, routes });
      expect(await screen.findByText(text)).toBeInTheDocument();
      unmount();
    }
    const me = mount({ path: "/volunteers/me", routes });
    expect(await screen.findByText("You aren't a member of any department.")).toBeInTheDocument();
    expect(
      await screen.findByLabelText("Show my playa name to others on my shifts"),
    ).not.toBeChecked();
    me.unmount();
  });

  it("an unqualified assignment names the missing qualifications from the param", async () => {
    mount({
      path: `/volunteers/shifts/${SHIFT}`,
      perms: ALL,
      routes: {
        [`GET ${V}/shifts/${SHIFT}`]: () =>
          json({
            id: SHIFT,
            department: DEPT,
            position: "p",
            label: "X",
            starts: "2099-07-01T15:00:00Z",
            ends: "2099-07-01T19:00:00Z",
            zone: "UTC",
            slots: 2,
          }),
        [`GET ${V}/shifts/${SHIFT}/roster`]: () => json({}),
        "GET /api/people/search": () =>
          json({ matches: [{ id: OTHER, playa_name: "Pat Dust", pronouns: "" }] }),
        [`POST ${V}/shifts/${SHIFT}/assign`]: () =>
          problem(409, {
            code: "conflict",
            fields: [
              { field: "person", code: "unqualified", params: { missing: QUAL }, reason: "" },
            ],
          }),
      },
    });
    await userEvent.type(await screen.findByLabelText("Playa name"), "Pat");
    await userEvent.click(screen.getByRole("button", { name: "Search" }));
    await userEvent.click(await screen.findByRole("button", { name: /Pat Dust/ }));
    await userEvent.click(screen.getByRole("button", { name: "Assign" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Missing: First aid.");
  });
});
