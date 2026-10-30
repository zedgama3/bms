import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { trackAction } from "@/usage";
import { BACKOFF_START_MS } from "./report";
import type { SendResult } from "./report";
import { getUsage, installUsage, uninstallUsageForTest, USAGE_MAX_PENDING } from "./usage";
import type { Usage, UsageBatch } from "./usage";

const T0 = new Date("2026-10-28T12:00:00.000Z");

interface Rig {
  usage: Usage;
  sent: { batch: UsageBatch; via: "fetch" | "beacon" }[];
  next: SendResult[];
}

function rig(actions: string[] = ["shift.filter", "dialog.open"]): Rig {
  const sent: Rig["sent"] = [];
  const next: SendResult[] = [];
  const usage = installUsage({
    appVersion: "v1.2.3",
    actions: new Set(actions),
    now: () => new Date(),
    vitals: false,
    send: (batch, via) => {
      sent.push({ batch: structuredClone(batch), via });
      return Promise.resolve(next.shift() ?? { kind: "accepted" });
    },
  });
  if (!usage) throw new Error("usage was not installed");
  return { usage, sent, next };
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(T0);
});
afterEach(() => {
  uninstallUsageForTest();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("usage collector", () => {
  it("F47: not installed under GPC or DNT", () => {
    for (const nav of [{ globalPrivacyControl: true }, { doNotTrack: "1" }]) {
      vi.stubGlobal("navigator", { ...navigator, ...nav });
      expect(installUsage({ appVersion: "dev", actions: new Set(), vitals: false })).toBeNull();
      expect(getUsage()).toBeNull();
      vi.unstubAllGlobals();
    }
    // nothing is delivered for an action either
    trackAction("shift.filter");
  });

  it("F48: screen_view and screen_ready batch, deduplicated with a count", async () => {
    const r = rig();
    r.usage.view("/hello");
    r.usage.view("/volunteers/shifts/{id}");
    r.usage.view("/hello");
    r.usage.view("/volunteers/shifts/{id}");
    r.usage.view("/hello");
    r.usage.ready("/hello", 412.6);
    await vi.advanceTimersByTimeAsync(10_000);
    expect(r.sent).toHaveLength(1);
    expect(r.sent[0]!.via).toBe("fetch");
    const events = r.sent[0]!.batch.events;
    expect(events[0]).toEqual({
      kind: "screen_view",
      screen: "/hello",
      from: "_entry",
      occurred_at: T0.toISOString(),
      app_version: "v1.2.3",
    });
    // the second view of the same pair merges into count
    const pairs = events
      .filter((e) => e.kind === "screen_view")
      .map((e) => [e.screen, e.from, e.count]);
    expect(pairs).toEqual([
      ["/hello", "_entry", undefined],
      ["/volunteers/shifts/{id}", "/hello", 2],
      ["/hello", "/volunteers/shifts/{id}", 2],
    ]);
    expect(events.find((e) => e.kind === "screen_ready")).toMatchObject({
      screen: "/hello",
      value: 413,
    });
  });

  it("sends nothing but tokens and numbers", () => {
    const r = rig();
    r.usage.view("/people/{person}");
    r.usage.view("/people/7d2c2f0e-0000-4000-8000-000000000001?x=1 name"); // not a pattern: dropped
    r.usage.flush("manual");
    const body = JSON.stringify(r.sent[0]!.batch);
    expect(body).not.toContain("7d2c2f0e");
    expect(r.sent[0]!.batch.events).toHaveLength(1);
  });

  it("F49: an undeclared action is dropped, a declared one is counted on the current screen", () => {
    const r = rig();
    r.usage.view("/volunteers/shifts");
    trackAction("shift.filter");
    trackAction("shift.filter");
    trackAction("made.up");
    r.usage.flush("manual");
    const actions = r.sent[0]!.batch.events.filter((e) => e.kind === "action");
    expect(actions).toEqual([
      expect.objectContaining({
        screen: "/volunteers/shifts",
        action: "shift.filter",
        count: 2,
      }),
    ]);
  });

  it("drops, never clamps, timings outside (0, 120000] ms", () => {
    const r = rig();
    r.usage.ready("/a", 0);
    r.usage.ready("/b", 120_001);
    r.usage.ready("/c", 120_000);
    r.usage.ready("/d", 0.2);
    r.usage.flush("manual");
    expect(r.sent[0]!.batch.events.map((e) => [e.screen, e.value])).toEqual([
      ["/c", 120_000],
      ["/d", 1],
    ]);
  });

  it("drops events that waited too long for the server to take them", async () => {
    const r = rig();
    r.next.push({ kind: "network" });
    r.usage.ready("/old", 100);
    r.usage.flush("manual");
    await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(24 * 3600_000);
    r.usage.ready("/new", 100);
    r.usage.flush("manual");
    const last = r.sent[r.sent.length - 1]!.batch.events;
    expect(last.map((e) => e.screen)).toEqual(["/new"]);
  });

  it("flushes at once when the page is hidden", () => {
    const r = rig();
    r.usage.view("/hello");
    vi.spyOn(document, "visibilityState", "get").mockReturnValue("hidden");
    document.dispatchEvent(new Event("visibilitychange"));
    expect(r.sent).toHaveLength(1);
    expect(r.sent[0]!.via).toBe("beacon");
  });

  it("pagehide sends by beacon", () => {
    const r = rig();
    r.usage.view("/hello");
    window.dispatchEvent(new Event("pagehide"));
    expect(r.sent).toHaveLength(1);
    expect(r.sent[0]!.via).toBe("beacon");
  });

  it("holds at most 50 events, dropping the oldest", () => {
    const r = rig();
    for (let i = 0; i < USAGE_MAX_PENDING + 5; i++) r.usage.ready(`/s${i}`, i);
    r.usage.flush("manual");
    const events = r.sent[0]!.batch.events;
    expect(events).toHaveLength(USAGE_MAX_PENDING);
    expect(events[0]!.screen).toBe("/s5");
  });

  it("F50: usage stops on 501 for the page load", async () => {
    const r = rig();
    r.next.push({ kind: "rejected", status: 501 });
    r.usage.view("/hello");
    r.usage.flush("manual");
    await vi.advanceTimersByTimeAsync(0);
    expect(r.usage.stopped()).toBe(true);
    r.usage.view("/other");
    trackAction("shift.filter");
    await vi.advanceTimersByTimeAsync(60_000);
    expect(r.sent).toHaveLength(1);
  });

  it("F50: usage stops on 404", async () => {
    const r = rig();
    r.next.push({ kind: "rejected", status: 404 });
    r.usage.view("/hello");
    r.usage.flush("manual");
    await vi.advanceTimersByTimeAsync(0);
    expect(r.usage.stopped()).toBe(true);
  });

  it("F50: usage backs off on 429 and on network failure", async () => {
    const r = rig();
    r.next.push({ kind: "rate_limited", retryAfterS: 60 });
    r.usage.view("/hello");
    r.usage.flush("manual");
    await vi.advanceTimersByTimeAsync(0);
    expect(r.sent).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(59_000);
    expect(r.sent).toHaveLength(1);
    r.next.push({ kind: "network" });
    await vi.advanceTimersByTimeAsync(2_000);
    expect(r.sent).toHaveLength(2); // retried after Retry-After, kept
    await vi.advanceTimersByTimeAsync(BACKOFF_START_MS - 2_000);
    expect(r.sent).toHaveLength(2);
    await vi.advanceTimersByTimeAsync(3_000);
    expect(r.sent).toHaveLength(3);
    expect(r.sent[2]!.batch.events[0]!.screen).toBe("/hello");
  });

  it("another 4xx drops the batch and keeps counting", async () => {
    const r = rig();
    r.next.push({ kind: "rejected", status: 400 });
    r.usage.view("/hello");
    r.usage.flush("manual");
    await vi.advanceTimersByTimeAsync(0);
    expect(r.usage.stopped()).toBe(false);
    r.usage.flush("manual");
    expect(r.sent).toHaveLength(1);
  });

  it("writes nothing to browser storage", () => {
    const r = rig();
    r.usage.view("/hello");
    r.usage.flush("manual");
    expect(localStorage.length).toBe(0);
    expect(sessionStorage.length).toBe(0);
  });
});
