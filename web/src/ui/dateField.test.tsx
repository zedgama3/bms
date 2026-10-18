import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { renderUi } from "@/test/render";
import { expectNoAxeViolations } from "@/test/axe";
import { Check, DateField, endOfDayInstant, lastDayOf, todayDate } from "@/ui";

/** Runs fn with the process in another time zone (Node re-reads TZ when it is assigned). */
function inZone<T>(tz: string, fn: () => T): T {
  const before = process.env["TZ"];
  process.env["TZ"] = tz;
  try {
    return fn();
  } finally {
    if (before === undefined) delete process.env["TZ"];
    else process.env["TZ"] = before;
  }
}

describe("endOfDayInstant (the end of the chosen day in the browser's zone)", () => {
  it.each([
    ["UTC", "2026-12-31", "2027-01-01T00:00:00.000Z"],
    ["America/Los_Angeles", "2026-12-31", "2027-01-01T08:00:00.000Z"],
    ["Pacific/Auckland", "2026-12-31", "2026-12-31T11:00:00.000Z"],
    ["America/Los_Angeles", "2026-03-07", "2026-03-08T08:00:00.000Z"], // the day before spring-forward
    ["Pacific/Auckland", "2028-02-28", "2028-02-28T11:00:00.000Z"],
  ])("%s: %s ends at %s", (tz, day, instant) => {
    expect(inZone(tz, () => endOfDayInstant(day))).toBe(instant);
  });

  it.each(["America/Los_Angeles", "Pacific/Auckland", "UTC"])(
    "%s: the last day shown is the day that was chosen",
    (tz) => {
      inZone(tz, () => {
        const end = endOfDayInstant("2026-12-31");
        const shown = new Intl.DateTimeFormat("en-CA", { dateStyle: "short" }).format(
          lastDayOf(end),
        );
        expect(shown).toBe("2026-12-31");
      });
    },
  );

  it.each(["", "2026-1-1", "tomorrow", "2026-13-45x"])("%j is no date", (v) => {
    expect(endOfDayInstant(v)).toBe("");
  });
});

describe("todayDate", () => {
  it("formats the browser's local day as a form value", () => {
    expect(todayDate(new Date(2026, 0, 5, 23, 59))).toBe("2026-01-05");
  });
});

describe("DateField and Check", () => {
  it("is a labelled native date input with a minimum, and reports its error", async () => {
    const { container } = renderUi(
      <DateField
        label="Ends on"
        name="ends_at"
        min="2026-10-18"
        error={{ text: "Choose a day that hasn't passed." }}
      />,
    );
    const input = screen.getByLabelText("Ends on");
    expect(input).toHaveAttribute("type", "date");
    expect(input).toHaveAttribute("min", "2026-10-18");
    expect(input).toHaveAttribute("aria-invalid", "true");
    await expectNoAxeViolations(container);
  });

  it("a Check is a 44 px control whose label toggles it", async () => {
    renderUi(<Check label="Include ended" hint="Past grants too" />);
    const box = screen.getByLabelText("Include ended");
    expect(box.className).toContain("size-11");
    expect(box).toHaveAccessibleDescription("Past grants too");
    await userEvent.click(screen.getByText("Include ended"));
    expect(box).toBeChecked();
  });
});
