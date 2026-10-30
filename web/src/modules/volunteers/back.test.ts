import { describe, expect, it } from "vitest";
import { backFrom, backTo } from "./back";

describe("back", () => {
  it("backTo keeps the path and the query", () => {
    expect(backTo("shifts", { pathname: "/volunteers", search: "?eligible=1" })).toEqual({
      back: { to: "/volunteers?eligible=1", kind: "shifts" },
    });
  });

  it("backFrom validates the shape and the path", () => {
    const fallback = { to: "/volunteers", kind: "shifts" };
    const bad: unknown[] = [
      undefined,
      null,
      "x",
      {},
      { back: null },
      { back: { to: "https://x", kind: "shifts" } },
      { back: { to: "/people/me", kind: "me" } },
      { back: { to: "/volunteers//x", kind: "shifts" } },
      { back: { to: "/volunteers", kind: "lead" } },
      { back: { to: "/volunteers#x", kind: "shifts" } },
      { back: { to: 5, kind: "shifts" } },
      { back: { to: "/volunteers/../people/me", kind: "shifts" } },
      { back: { to: "/volunteers/departments/%2e%2e/x", kind: "department" } },
      { back: { to: "/volunteers/departments/%2E/x", kind: "department" } },
      { back: { to: "/volunteers/departments/./x", kind: "department" } },
      { back: { to: "/volunteers\\evil", kind: "shifts" } },
      { back: { to: "/volunteers/departments/a%5Cb", kind: "department" } },
      { back: { to: "/volunteers/me", kind: "shifts" } },
      { back: { to: "/volunteers", kind: "me" } },
      { back: { to: "/volunteers", kind: "department" } },
    ];
    for (const s of bad) expect(backFrom(s)).toEqual(fallback);
    const good = { back: { to: "/volunteers/departments/d1?tab=shifts", kind: "department" } };
    expect(backFrom(good)).toEqual(good.back);
    expect(backFrom({ back: { to: "/volunteers/me", kind: "me" } }).kind).toBe("me");
  });
});
