import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@/api";
import type { ApiErrorInit } from "@/api/errors";
import {
  installReporter,
  scrub,
  setCurrentScreen,
  toPattern,
  uninstallReporterForTest,
} from "./report";
import type { Batch, Reporter, SendResult, Sender } from "./report";

const T0 = new Date("2026-10-17T12:00:00.000Z");
const MARKER = "ZQ9MARKER7";
const EMAIL = "zq9@marker.example";

function err(over: Partial<ApiErrorInit> = {}): ApiError {
  return new ApiError({
    status: 500,
    code: "internal",
    method: "GET",
    route: "/api/hello/greeting",
    module: "hello",
    traceId: "0123456789abcdef0123456789abcdef",
    ref: "1A2B-3C4D",
    ...over,
  });
}

interface Rig {
  reporter: Reporter;
  sent: { batch: Batch; via: "fetch" | "beacon" }[];
  next: SendResult[];
  clock: { t: Date };
}

function rig(version = "v1.2.3"): Rig {
  const sent: Rig["sent"] = [];
  const next: SendResult[] = [];
  const clock = { t: new Date(T0) };
  const send: Sender = (batch, via) => {
    sent.push({ batch: structuredClone(batch), via });
    return Promise.resolve(next.shift() ?? { kind: "accepted" });
  };
  const reporter = installReporter({ appVersion: version, now: () => new Date(clock.t), send });
  return { reporter, sent, next, clock };
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(T0);
});
afterEach(() => {
  uninstallReporterForTest();
  vi.useRealTimers();
});

describe("reportable (decision 14)", () => {
  it.each([
    ["network", true],
    ["malformed_response", true],
    ["internal", true],
    ["cross_origin", true],
    ["misdirected", true],
    ["method_not_allowed", true],
    ["payload_too_large", true],
    ["unsupported_media_type", true],
    ["invalid", false],
    ["unauthenticated", false],
    ["permission", false],
    ["not_found", false],
    ["conflict", false],
    ["rate_limited", false],
    ["unsupported", false],
    ["unavailable", false],
    ["timeout", false],
    ["read_only", false],
    ["shutting_down", false],
  ])("%s -> %s", (code, want) => {
    const { reporter } = rig();
    expect(reporter.reportable(err({ code }))).toBe(want);
  });

  it("never reports the intake's own failures", () => {
    const { reporter } = rig();
    expect(reporter.reportable(err({ route: "/api/client-errors" }))).toBe(false);
  });
});

describe("api failures", () => {
  it("F10: internal is reported with ref and trace id, after 10 s", async () => {
    const r = rig();
    setCurrentScreen("/hello");
    r.reporter.apiFailure(err());
    expect(r.sent).toHaveLength(0);
    await vi.advanceTimersByTimeAsync(10_000);
    expect(r.sent).toHaveLength(1);
    const rep = r.sent[0]!.batch.reports[0]!;
    expect(rep).toMatchObject({
      kind: "api",
      screen: "/hello",
      app_version: "v1.2.3",
      error_ref: "1A2B-3C4D",
      trace_id: "0123456789abcdef0123456789abcdef",
      api: { method: "GET", route: "/api/hello/greeting", status: 500, code: "internal" },
      count: 1,
    });
    expect(rep.occurred_at).toBe(T0.toISOString());
  });

  it("sends the real client or shell code as api.code (intake accepts any token)", async () => {
    const r = rig();
    r.reporter.apiFailure(err({ code: "cross_origin", status: 403, ref: undefined }));
    r.reporter.apiFailure(
      err({ code: "network", status: 0, ref: undefined, route: "/api/modules" }),
    );
    r.reporter.flush("manual");
    await vi.advanceTimersByTimeAsync(0);
    const reps = r.sent[0]!.batch.reports;
    expect(reps.map((x) => x.api?.code)).toEqual(["cross_origin", "network"]);
    expect(reps[0]!.message).toContain("cross_origin");
    expect(reps[1]!.api?.status).toBe(0);
  });

  it("marks the error reported and does not queue it twice", () => {
    const r = rig();
    const e = err();
    r.reporter.apiFailure(e);
    r.reporter.apiFailure(e);
    expect(e.reported).toBe(true);
    r.reporter.flush("manual");
    expect(r.sent[0]!.batch.reports).toHaveLength(1);
    expect(r.sent[0]!.batch.reports[0]!.count).toBe(1);
  });

  it("does not report an unreportable code", () => {
    const r = rig();
    r.reporter.apiFailure(err({ code: "not_found", status: 404 }));
    r.reporter.flush("manual");
    expect(r.sent).toHaveLength(0);
  });
});

describe("crash and rejection sources", () => {
  it("window error becomes a crash", () => {
    const r = rig();
    const e = new Error("kaboom");
    window.dispatchEvent(new ErrorEvent("error", { message: "kaboom", error: e }));
    r.reporter.flush("manual");
    const rep = r.sent[0]!.batch.reports[0]!;
    expect(rep.kind).toBe("crash");
    expect(rep.message).toBe("kaboom");
    expect(rep.stack).toContain("kaboom");
  });

  it("unhandledrejection becomes a rejection; an already reported ApiError is skipped", () => {
    const r = rig();
    const reported = err();
    reported.reported = true;
    const evt = (reason: unknown) => {
      const ev = new Event("unhandledrejection") as Event & { reason: unknown };
      ev.reason = reason;
      window.dispatchEvent(ev);
    };
    evt(reported);
    evt(new DOMException("a", "AbortError"));
    evt(new Error("later"));
    r.reporter.flush("manual");
    const reps = r.sent[0]!.batch.reports;
    expect(reps).toHaveLength(1);
    expect(reps[0]).toMatchObject({ kind: "rejection", message: "Error: later" });
  });

  it("screenCrash skips an ApiError already reported: no double count", () => {
    const r = rig();
    const e = err();
    r.reporter.apiFailure(e);
    r.reporter.screenCrash(e, "/hello");
    r.reporter.flush("manual");
    expect(r.sent[0]!.batch.reports).toHaveLength(1);
    expect(r.sent[0]!.batch.reports[0]).toMatchObject({ kind: "api", count: 1 });
  });

  it("screenCrash queues a screen report with the pattern", () => {
    const r = rig();
    r.reporter.screenCrash(new Error("render failed"), "/people/:id");
    r.reporter.flush("manual");
    expect(r.sent[0]!.batch.reports[0]).toMatchObject({ kind: "screen", screen: "/people/{id}" });
  });
});

describe("shape and privacy", () => {
  it("strips every ?query from messages and stacks and never carries personal markers", () => {
    const r = rig();
    const e = new Error(`failed at http://x/people?name=${MARKER}&mail=${EMAIL}`);
    e.stack = `Error: x\n    at f (http://x/assets/a.js?v=${MARKER}:10:5)`;
    window.dispatchEvent(new ErrorEvent("error", { message: e.message, error: e }));
    r.reporter.flush("manual");
    const body = JSON.stringify(r.sent[0]!.batch);
    expect(body).not.toContain(MARKER);
    expect(body).not.toContain(EMAIL);
  });

  it("cuts message and stack to 512 and 4096 bytes", () => {
    expect(new TextEncoder().encode(scrub("é".repeat(1000), 512)).length).toBeLessThanOrEqual(512);
    expect(scrub("a".repeat(5000), 4096)).toHaveLength(4096);
  });

  it("F17: every producible pattern matches the token grammar", () => {
    const token = /^[A-Za-z0-9/_][A-Za-z0-9._:/{}-]{0,127}$/;
    for (const p of ["/", "/hello", "/people/:id", "/a/:b/c/:d", "/admin/modules", "/*"]) {
      const out = toPattern(p);
      expect(out, p).toBeDefined();
      expect(out!).toMatch(token);
    }
    expect(toPattern("/has space")).toBeUndefined();
    expect(toPattern("/x?y=1")).toBeUndefined();
  });

  it("omits the screen before the router mounted", () => {
    const r = rig();
    setCurrentScreen(undefined);
    r.reporter.apiFailure(err());
    r.reporter.flush("manual");
    expect(r.sent[0]!.batch.reports[0]).not.toHaveProperty("screen");
  });
});

describe("dedup, cap and batching", () => {
  it("a repeat increments count (capped at 1000)", () => {
    const r = rig();
    for (let i = 0; i < 1200; i++) r.reporter.apiFailure(err({ route: "/api/hello/greeting" }));
    // apiFailure marks the first error reported; new ApiErrors with the same shape dedupe
    r.reporter.flush("manual");
    expect(r.sent[0]!.batch.reports).toHaveLength(1);
    expect(r.sent[0]!.batch.reports[0]!.count).toBe(1000);
  });

  it("F16: more than 100 distinct pending drops the oldest", async () => {
    const r = rig();
    for (let i = 0; i < 101; i++) r.reporter.screenCrash(new Error(`e${i}`), "/x");
    for (let i = 0; i < 6; i++) {
      r.reporter.flush("manual"); // 5 batches of 20 empty 100 reports; the sixth finds none
      await vi.advanceTimersByTimeAsync(0);
    }
    const messages = r.sent.flatMap((s) => s.batch.reports.map((x) => x.message));
    expect(messages).not.toContain("Error: e0");
    expect(messages).toContain("Error: e100");
  });

  it("sends at most 20 reports per batch, oldest first", () => {
    const r = rig();
    for (let i = 0; i < 45; i++) r.reporter.screenCrash(new Error(`e${i}`), "/x");
    r.reporter.flush("manual");
    expect(r.sent[0]!.batch.reports).toHaveLength(20);
    expect(r.sent[0]!.batch.reports[0]!.message).toBe("Error: e0");
  });

  it("the 10 s timer runs only while something is pending", async () => {
    const r = rig();
    expect(vi.getTimerCount()).toBe(0);
    r.reporter.screenCrash(new Error("one"), "/x");
    expect(vi.getTimerCount()).toBe(1);
    await vi.advanceTimersByTimeAsync(10_000);
    expect(r.sent).toHaveLength(1);
    expect(vi.getTimerCount()).toBe(0);
  });

  it("an empty app_version is omitted rather than sent invalid", () => {
    const r = rig("not a token");
    r.reporter.screenCrash(new Error("x"), "/x");
    r.reporter.flush("manual");
    expect(r.sent[0]!.batch.reports[0]).not.toHaveProperty("app_version");
  });
});

describe("delivery", () => {
  it("pagehide uses the beacon path", () => {
    const r = rig();
    r.reporter.screenCrash(new Error("x"), "/x");
    window.dispatchEvent(new Event("pagehide"));
    expect(r.sent[0]!.via).toBe("beacon");
  });

  it("visibilitychange to hidden flushes via beacon", () => {
    const r = rig();
    r.reporter.screenCrash(new Error("x"), "/x");
    Object.defineProperty(document, "visibilityState", { configurable: true, get: () => "hidden" });
    document.dispatchEvent(new Event("visibilitychange"));
    Object.defineProperty(document, "visibilityState", {
      configurable: true,
      get: () => "visible",
    });
    expect(r.sent[0]!.via).toBe("beacon");
  });

  it("F13: 429 pauses for Retry-After and keeps pending", async () => {
    const r = rig();
    r.next.push({ kind: "rate_limited", retryAfterS: 90 });
    r.reporter.screenCrash(new Error("x"), "/x");
    await vi.advanceTimersByTimeAsync(10_000);
    expect(r.sent).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(60_000);
    r.clock.t = new Date(T0.getTime() + 70_000);
    expect(r.sent).toHaveLength(1); // still paused at +70 s
    r.clock.t = new Date(T0.getTime() + 100_000);
    await vi.advanceTimersByTimeAsync(40_000);
    expect(r.sent.length).toBeGreaterThanOrEqual(2);
    expect(r.sent[1]!.batch.reports[0]!.message).toBe("Error: x");
  });

  it("F14: 400 drops the batch", async () => {
    const r = rig();
    r.next.push({ kind: "rejected" });
    r.reporter.screenCrash(new Error("x"), "/x");
    await vi.advanceTimersByTimeAsync(10_000);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(r.sent).toHaveLength(1);
  });

  it("F15: a network failure backs off 10 s doubling to 5 min and is never reported", async () => {
    const r = rig();
    const calls: number[] = [];
    r.next.push({ kind: "network" }, { kind: "network" }, { kind: "network" });
    r.reporter.screenCrash(new Error("x"), "/x");
    for (let i = 0; i < 400; i++) {
      r.clock.t = new Date(r.clock.t.getTime() + 1000);
      await vi.advanceTimersByTimeAsync(1000);
      if (r.sent.length > calls.length) calls.push(i + 1);
    }
    expect(r.sent.length).toBeGreaterThanOrEqual(4);
    // attempts at ~10 s, then +10, +20, +40 s ...
    expect(calls[1]! - calls[0]!).toBeGreaterThanOrEqual(10);
    expect(calls[2]! - calls[1]!).toBeGreaterThanOrEqual(20);
    expect(calls[3]! - calls[2]!).toBeGreaterThanOrEqual(40);
    expect(JSON.stringify(r.sent)).not.toContain("client-errors");
  });

  it("F18: a throwing sender never propagates", async () => {
    const reporter = installReporter({
      appVersion: "v1",
      send: () => {
        throw new Error("sender exploded");
      },
    });
    reporter.screenCrash(new Error("x"), "/x");
    expect(() => reporter.flush("manual")).not.toThrow();
    await vi.advanceTimersByTimeAsync(10_000);
  });

  it("F18: a rejecting sender never propagates", async () => {
    const reporter = installReporter({
      appVersion: "v1",
      send: () => Promise.reject(new Error("no")),
    });
    reporter.screenCrash(new Error("x"), "/x");
    reporter.flush("manual");
    await vi.advanceTimersByTimeAsync(10_000);
  });

  it("is idempotent", () => {
    const a = installReporter({ appVersion: "v1" });
    const b = installReporter({ appVersion: "v2" });
    expect(a).toBe(b);
  });
});

describe("untranslated codes (decision 2)", () => {
  it("reports once per page per code, with the code in the message", () => {
    const r = rig();
    const e = err({ status: 400, code: "invalid", ref: undefined });
    r.reporter.untranslated("field", "overlap", e);
    r.reporter.untranslated("field", "overlap", e);
    r.reporter.untranslated("error", "teapot", e);
    r.reporter.flush("manual");
    const msgs = r.sent[0]!.batch.reports.map((x) => x.message);
    expect(msgs).toEqual(["untranslated field code overlap", "untranslated error code teapot"]);
    expect(r.sent[0]!.batch.reports[0]!.api).toMatchObject({
      method: "GET",
      status: 400,
      code: "invalid",
    });
  });

  it("never copies an odd-looking code into the report", () => {
    const r = rig();
    r.reporter.untranslated("field", `${EMAIL} ${MARKER}`, err());
    r.reporter.flush("manual");
    expect(JSON.stringify(r.sent[0]!.batch)).not.toContain(MARKER);
  });
});
