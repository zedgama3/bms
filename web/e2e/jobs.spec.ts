// End-to-end smoke run, the background work screen (web-shell.md decision 22, "Test plan").
import { expect } from "@playwright/test";
import {
  noHorizontalScroll,
  axe,
  touchTargets,
  mockMe,
  press,
  test,
  only,
  adminSession,
} from "./support/helpers";

test.describe("jobs: the background work screen", () => {
  const FAILED = "5b1f0000-0000-4000-8000-000000000001";
  const record = {
    id: FAILED,
    name: "audit.purge",
    status: "failed",
    attempts: 3,
    run_at: "2026-10-19T10:00:00Z",
    last_error: "gave_up",
  };

  test("status, run now and retry, with touch alone", async ({ page }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    if (isPhone) await page.setViewportSize({ width: 320, height: 568 });
    await page.route("**/api/accounts/session", (r) =>
      r.fulfill({ json: adminSession(["jobs.read", "jobs.manage"]) }),
    );
    await mockMe(page);
    const calls: string[] = [];
    await page.route("**/api/jobs/", (r) =>
      r.fulfill({
        json: {
          counts: { failed: 1 },
          read_only: false,
          instance: "main",
          registered: [
            {
              name: "jobs.purge",
              owner: "jobs",
              every_seconds: 3600,
              timeout_seconds: 600,
              next_due: "2026-10-20T12:00:00Z",
            },
          ],
          failed: [record],
          running: [],
          retrying: [],
          waiting: [],
        },
      }),
    );
    await page.route("**/api/jobs/registered/jobs.purge/run", (r) => {
      calls.push("run");
      return r.fulfill({ status: 202, json: { id: FAILED } });
    });
    await page.route(`**/api/jobs/queue/${FAILED}/retry`, (r) => {
      calls.push("retry");
      return r.fulfill({ status: 204 });
    });
    await page.goto("/jobs");
    await expect(page.getByRole("heading", { level: 1, name: "Background work" })).toBeVisible();
    await expect(page.getByText("Runs every 1 hour")).toBeVisible();
    await expect(page.getByText("The job kept failing and was given up on")).toBeVisible();
    await expect(page.getByText("audit.purge")).toHaveCount(0); // titles, never raw names
    if (isPhone) {
      await noHorizontalScroll(page);
      await touchTargets(page);
    } else {
      // At 1280 px no table header wraps: each th is as tall as one line of its text.
      const heights = await page.locator("th[scope=col]").evaluateAll((ths) =>
        ths.map((th) => {
          const cs = getComputedStyle(th);
          const line = parseFloat(cs.lineHeight) || parseFloat(cs.fontSize) * 1.5;
          const pad = parseFloat(cs.paddingTop) + parseFloat(cs.paddingBottom);
          const border = parseFloat(cs.borderBottomWidth);
          return th.getBoundingClientRect().height - pad - border - line;
        }),
      );
      expect(heights.length).toBeGreaterThan(0);
      for (const extra of heights) expect(Math.abs(extra)).toBeLessThan(2);
    }
    await axe(page);
    await press(
      page.getByRole("button", { name: "Run now: Delete finished job records" }),
      isPhone,
    );
    await press(
      page.getByRole("button", { name: /^Retry Delete audit entries past their retention/ }),
      isPhone,
    );
    await expect.poll(() => calls).toEqual(["run", "retry"]);
  });

  test("a participant gets the forbidden state and no entry", async ({ page }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    await page.route("**/api/accounts/session", (r) => r.fulfill({ json: adminSession([]) }));
    await mockMe(page);
    await page.goto("/jobs");
    await expect(page.getByText("You don't have access to this screen.")).toBeVisible();
    if (isPhone) await page.getByRole("button", { name: "Menu" }).tap();
    await expect(page.getByRole("link", { name: "Background work" })).toHaveCount(0);
    await axe(page);
  });
});
