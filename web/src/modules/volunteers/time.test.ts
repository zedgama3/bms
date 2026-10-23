import { describe, expect, it } from "vitest";
import { ledDepartments, recordStatus } from "./helpers";
import { dayKey, fromLocalInput, toLocalInput } from "./time";

describe("zoned times", () => {
  it("reads wall-clock time in the zone and writes it back", () => {
    expect(fromLocalInput("2099-07-01T09:00", "America/Denver")).toBe("2099-07-01T15:00:00.000Z");
    expect(toLocalInput("2099-07-01T15:00:00Z", "America/Denver")).toBe("2099-07-01T09:00");
    expect(fromLocalInput("2099-01-10T09:00", "America/Denver")).toBe("2099-01-10T16:00:00.000Z");
  });
  it("groups by the day in the shift's zone, not the browser's", () => {
    expect(dayKey("2099-07-02T03:00:00Z", "America/Denver")).toBe("2099-07-01");
  });
  it("a malformed value is empty; an unknown zone falls back to UTC", () => {
    expect(fromLocalInput("nope", "UTC")).toBe("");
    expect(fromLocalInput("2099-07-01T09:00", "Not/AZone")).toBe("2099-07-01T09:00:00.000Z");
  });
});

describe("helpers", () => {
  it("record status", () => {
    expect(recordStatus({}, "2026-10-21")).toBe("pending");
    expect(recordStatus({ verified_at: "x" }, "2026-10-21")).toBe("verified");
    expect(recordStatus({ verified_at: "x", expires_on: "2026-10-20" }, "2026-10-21")).toBe(
      "expired",
    );
    expect(recordStatus({ verified_by: "p", expires_on: "2026-10-21" }, "2026-10-21")).toBe(
      "verified",
    );
  });
  it("led departments", () => {
    const s = {
      signedIn: true as const,
      personId: "p",
      accountId: "a",
      playaName: "",
      superuser: false,
      passwordChangeRequired: false,
      expiresAt: null,
      signupOpen: false,
      permissions: ["x.a@d1", "x.b", "y.c@d2"],
    };
    expect(ledDepartments(s, ["x.a"])).toEqual({ site: false, ids: ["d1"] });
    expect(ledDepartments(s, ["x.b", "y.c"])).toEqual({ site: true, ids: ["d2"] });
    expect(ledDepartments({ signedIn: false, signupOpen: false }, ["x.a"])).toEqual({
      site: false,
      ids: [],
    });
  });
});
