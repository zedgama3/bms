import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { mountApp } from "@/test/app";
import { installUsage, uninstallUsageForTest } from "./usage";
import type { UsageBatch, UsageEvent } from "./usage";

afterEach(() => uninstallUsageForTest());

function collect() {
  const sent: UsageEvent[] = [];
  const usage = installUsage({
    appVersion: "dev",
    actions: new Set(),
    vitals: false,
    send: (b: UsageBatch) => {
      sent.push(...b.events);
      return Promise.resolve({ kind: "accepted" });
    },
  });
  if (!usage) throw new Error("not installed");
  return { usage, sent };
}

describe("usage tracker", () => {
  it("F48: records one screen_view with from and one screen_ready per settled screen", async () => {
    const { usage, sent } = collect();
    const { router } = mountApp();
    await screen.findByRole("heading", { level: 1, name: "Welcome to BMS" });
    await waitFor(() => expect(usage.current()).toBe("/"));
    await router()!.navigate("/hello");
    await waitFor(() => expect(usage.current()).toBe("/hello"));
    // wait for ready of /hello
    await waitFor(() => {
      usage.flush("manual");
      expect(sent.filter((e) => e.kind === "screen_ready").map((e) => e.screen)).toContain(
        "/hello",
      );
    });
    const views = sent.filter((e) => e.kind === "screen_view");
    expect(views.map((e) => [e.screen, e.from])).toEqual([
      ["/", "_entry"],
      ["/hello", "/"],
    ]);
    for (const e of sent.filter((e) => e.kind === "screen_ready")) {
      expect(typeof e.value).toBe("number");
    }
  });

  it("a query-string change is not a new screen view", async () => {
    const { usage, sent } = collect();
    const { router } = mountApp({ path: "/hello" });
    await waitFor(() => expect(usage.current()).toBe("/hello"));
    await router()!.navigate("/hello?filter=zq9");
    await new Promise((r) => setTimeout(r, 150));
    usage.flush("manual");
    expect(sent.filter((e) => e.kind === "screen_view")).toHaveLength(1);
    expect(JSON.stringify(sent)).not.toContain("zq9");
  });

  it("only the final screen of a redirect is a view", async () => {
    const { usage, sent } = collect();
    // /people needs a session: the gate redirects a signed-out visitor to sign-in
    mountApp({ path: "/people/me" });
    await waitFor(() => expect(usage.current()).toBe("/accounts/sign-in"));
    usage.flush("manual");
    expect(sent.filter((e) => e.kind === "screen_view").map((e) => e.screen)).toEqual([
      "/accounts/sign-in",
    ]);
  });

  it("does nothing when usage is not installed", async () => {
    mountApp();
    await screen.findByRole("heading", { level: 1 });
  });
});
