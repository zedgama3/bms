import { describe, expect, it } from "vitest";
import { isOutsideDates } from "./helpers";

const dates = { start: "2026-07-10T00:00:00Z", end: "2026-07-14T00:00:00Z" };

// departments-events decision 10 (D57): exactly End is inside.
describe("TestOutsideDatesWarning_Table", () => {
  const cases: {
    name: string;
    starts_at: string;
    ends_at?: string;
    outside: boolean;
  }[] = [
    { name: "moment before Start", starts_at: "2026-07-09T23:59:59Z", outside: true },
    { name: "moment at Start", starts_at: dates.start, outside: false },
    { name: "moment inside", starts_at: "2026-07-12T00:00:00Z", outside: false },
    { name: "moment at End", starts_at: dates.end, outside: false },
    { name: "moment after End", starts_at: "2026-07-14T00:00:01Z", outside: true },
    {
      name: "window starting before Start",
      starts_at: "2026-07-09T00:00:00Z",
      ends_at: "2026-07-11T00:00:00Z",
      outside: true,
    },
    {
      name: "window inside",
      starts_at: "2026-07-11T00:00:00Z",
      ends_at: "2026-07-12T00:00:00Z",
      outside: false,
    },
    {
      name: "window ending at End",
      starts_at: "2026-07-13T00:00:00Z",
      ends_at: dates.end,
      outside: false,
    },
    {
      name: "window ending after End",
      starts_at: "2026-07-13T00:00:00Z",
      ends_at: "2026-07-14T00:00:01Z",
      outside: true,
    },
  ];
  for (const c of cases)
    it(c.name, () => {
      const item =
        c.ends_at === undefined
          ? { starts_at: c.starts_at }
          : { starts_at: c.starts_at, ends_at: c.ends_at };
      expect(isOutsideDates(item, dates)).toBe(c.outside);
    });
});
