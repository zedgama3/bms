import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";
import type { Locator, Page } from "@playwright/test";

const mode = process.env["BMS_E2E_MODE"] ?? "default";
const only = (m: string) => test.skip(mode !== m, `needs BMS_E2E_MODE=${m}`);

/** Console messages that mean the CSP blocked something. */
function watchCsp(page: Page): string[] {
  const violations: string[] = [];
  page.on("console", (m) => {
    if (/content security policy|refused to (apply|load|execute|connect)/i.test(m.text()))
      violations.push(m.text());
  });
  page.on("pageerror", (e) => violations.push(`pageerror: ${e.message}`));
  return violations;
}

async function noHorizontalScroll(page: Page) {
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  expect(overflow, "horizontal overflow in CSS px").toBeLessThanOrEqual(0);
}

async function axe(page: Page) {
  const results = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"])
    .analyze();
  expect(
    results.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`),
  ).toEqual([]);
}

/** On a phone every control is at least 44 x 44 CSS px. */
async function touchTargets(page: Page) {
  const small = await page.evaluate(() => {
    const out: string[] = [];
    for (const el of document.querySelectorAll<HTMLElement>(
      "a[href], button, input:not([type=hidden]), select, textarea, summary",
    )) {
      if (el.closest(".sr-only") || el.classList.contains("sr-only")) continue;
      const r = el.getBoundingClientRect();
      if (r.width === 0 || r.height === 0) continue;
      if (r.width < 44 || r.height < 44)
        out.push(
          `${el.tagName.toLowerCase()} "${(el.textContent ?? "").trim().slice(0, 30)}" ${Math.round(r.width)}x${Math.round(r.height)}`,
        );
    }
    return out;
  });
  expect(small, "targets under 44 x 44 px").toEqual([]);
}

/** Opens the navigation on a phone, where it is a disclosure button. */
async function openMenu(page: Page, isPhone: boolean): Promise<Locator> {
  if (isPhone) {
    const menu = page.getByRole("button", { name: "Menu" });
    await expect(menu).toHaveAttribute("aria-expanded", "false");
    await menu.tap();
    await expect(menu).toHaveAttribute("aria-expanded", "true");
  }
  return page.getByRole("navigation", { name: "Main" });
}

test.describe("the frame", () => {
  test("renders with a skip link and landmarks; on a phone the Menu disclosure opens and closes on navigation", async ({
    page,
  }, info) => {
    only("default");
    const csp = watchCsp(page);
    const res = await page.goto("/");
    expect(res?.headers()["content-security-policy"]).toContain("default-src 'self'");
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    await expect(page.locator("header")).toHaveCount(1);
    await expect(page.locator("main#content")).toHaveCount(1);
    await expect(page.locator("footer")).toHaveCount(1);
    await expect(page.locator("footer small")).toContainText("Version");
    const first = await page.evaluate(() =>
      document.querySelector("a, button, input, select")?.textContent?.trim(),
    );
    expect(first).toBe("Skip to content");

    const isPhone = info.project.name === "phone";
    const nav = await openMenu(page, isPhone);
    await expect(nav.getByRole("link", { name: "Hello" })).toBeVisible();
    if (isPhone) await nav.getByRole("link", { name: "Hello" }).tap();
    else await nav.getByRole("link", { name: "Hello" }).click();
    await expect(page).toHaveURL(/\/hello$/);
    if (isPhone)
      await expect(page.getByRole("button", { name: "Menu" })).toHaveAttribute(
        "aria-expanded",
        "false",
      );
    await expect(page.getByRole("heading", { level: 1 })).toBeFocused();
    expect(csp).toEqual([]);
  });

  test("the CSP header is on / and no CSP violation is logged across the shell's screens", async ({
    page,
  }) => {
    only("default");
    const csp = watchCsp(page);
    const res = await page.goto("/");
    expect(res?.headers()["content-security-policy"]).toMatch(/default-src 'self'/);
    for (const path of ["/hello", "/sign-in", "/admin/modules", "/nowhere"]) {
      await page.goto(path);
      await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    }
    expect(csp).toEqual([]);
  });
});

test.describe("hello", () => {
  test("the entry appears and its screen calls GET /api/hello/public-greeting", async ({
    page,
  }) => {
    only("default");
    const called = page.waitForResponse(
      (r) =>
        new URL(r.url()).pathname === "/api/hello/public-greeting" &&
        r.request().method() === "GET",
    );
    await page.goto("/hello");
    expect((await called).status()).toBe(200);
    await expect(page.getByRole("heading", { level: 1, name: /!$/ })).toBeVisible();
    await expect(page.getByRole("button", { name: "Say hello" })).toHaveCount(0); // M0: signed out, public part only
  });

  test("with the module disabled the entry is gone and /hello is not found", async ({ page }) => {
    only("hello-disabled");
    await page.goto("/");
    await expect(page.getByRole("link", { name: "Hello" })).toHaveCount(0);
    await page.goto("/hello");
    await expect(page.getByRole("heading", { level: 1, name: "Not found" })).toBeVisible();
  });

  test("a 401 lands on /sign-in?returnTo=/hello and, in M0, says sign-in isn't available", async ({
    page,
  }) => {
    only("default");
    // Force the signed-out answer through the real client: the screen's call gets a 401.
    await page.route("**/api/hello/public-greeting", (route) =>
      route.fulfill({
        status: 401,
        contentType: "application/problem+json",
        body: JSON.stringify({ status: 401, code: "unauthenticated" }),
      }),
    );
    await page.goto("/hello");
    await expect(page).toHaveURL(/\/sign-in\?returnTo=%2Fhello$/);
    await expect(page.getByRole("heading", { level: 1, name: "Sign in" })).toBeVisible();
    await expect(page.getByText("Sign-in isn't available in this build yet.")).toBeVisible();
  });
});

test.describe("read-only copy", () => {
  test("the banner appears after the first call and the server's X-Read-Only is true", async ({
    page,
  }) => {
    only("readonly");
    const modules = page.waitForResponse((r) => new URL(r.url()).pathname === "/api/modules");
    await page.goto("/");
    expect((await modules).headers()["x-read-only"]).toBe("true");
    await expect(page.getByText(/This copy is read-only/)).toBeVisible();
  });
});

const screens: [string, string][] = [
  ["home", "/"],
  ["sign-in", "/sign-in"],
  ["hello", "/hello"],
  ["not found", "/nowhere"],
];

test.describe("accessibility and the phone layout", () => {
  for (const scheme of ["light", "dark"] as const) {
    for (const [name, path] of screens) {
      test(`${name} has no axe violations (${scheme})`, async ({ page }) => {
        only("default");
        await page.emulateMedia({ colorScheme: scheme });
        await page.goto(path);
        await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
        await axe(page);
      });
    }
  }

  for (const [name, path] of screens) {
    test(`${name}: no horizontal scroll, 44 px targets and the heading in the first viewport on a phone`, async ({
      page,
    }, info) => {
      only("default");
      test.skip(info.project.name !== "phone", "the phone project only");
      await page.goto(path);
      await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
      await noHorizontalScroll(page);
      await touchTargets(page);
      // The first viewport shows the heading without scrolling; each screen's primary action
      // follows it directly (home: the sign-in or the cards; sign-in: the form).
      const inView = await page.evaluate(() => {
        const h1 = document.querySelector("h1")?.getBoundingClientRect();
        return !!h1 && h1.bottom <= window.innerHeight;
      });
      expect(inView).toBe(true);
    });

    test(`${name}: works at 320 px wide with 200% text`, async ({ page }, info) => {
      only("default");
      test.skip(info.project.name !== "phone", "the phone project only");
      await page.setViewportSize({ width: 320, height: 568 });
      await page.goto(path);
      await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
      await noHorizontalScroll(page);
      // Text at 200% (WCAG 1.4.4) still reflows. The CSP forbids an injected <style> or style
      // attribute, so the rule arrives as a same-origin stylesheet that the test serves.
      await page.route("**/e2e-zoom.css", (route) =>
        route.fulfill({ contentType: "text/css", body: "html { font-size: 200%; }" }),
      );
      await page.evaluate(
        () =>
          new Promise<void>((resolve, reject) => {
            const link = document.createElement("link");
            link.rel = "stylesheet";
            link.href = "/e2e-zoom.css";
            link.onload = () => resolve();
            link.onerror = () => reject(new Error("zoom stylesheet blocked"));
            document.head.append(link);
          }),
      );
      const fontPx = await page.evaluate(() =>
        parseFloat(getComputedStyle(document.documentElement).fontSize),
      );
      expect(fontPx).toBeGreaterThanOrEqual(30);
      await noHorizontalScroll(page);
      await touchTargets(page);
    });
  }
});

test.describe("/admin/modules (signed in, mocked at the network)", () => {
  const person = {
    signed_in: true,
    person_id: "7d2c2f0e-0000-4000-8000-000000000001",
    playa_name: "Dusty",
    superuser: false,
    permissions: ["core.modules.read"],
  };
  const body = {
    version: "dev",
    graph: {
      modules: [
        {
          name: "hello",
          enabled: true,
          has_schema: true,
          permissions: ["hello.greetings.read", "hello.greetings.write"],
          settings: ["hello.greeting"],
          publishes: ["hello.greeted"],
          subscribes: ["hello.greeted"],
          provides: ["greeter"],
          requires: [],
          uses: [],
          defines: ["hello.banner"],
          contributes_to: ["hello.banner"],
        },
      ],
      edges: [{ from: "hello", to: "core", kind: "uses", via: "greeter" }],
      warnings: [
        {
          module: "hello",
          message: "a long operator warning that has to wrap on a small phone screen",
        },
      ],
    },
    schema: [{ set: "hello", built: 1, applied: 1, current: true }],
  };

  for (const scheme of ["light", "dark"] as const) {
    test(`lays out at phone width, 44 px targets, axe clean (${scheme})`, async ({
      page,
    }, info) => {
      only("default");
      await page.emulateMedia({ colorScheme: scheme });
      await page.route("**/api/accounts/session", (r) => r.fulfill({ json: person }));
      await page.route("**/api/admin/modules", (r) => r.fulfill({ json: body }));
      await page.goto("/admin/modules");
      await expect(page.getByRole("heading", { level: 1, name: "Modules" })).toBeVisible();
      await expect(page.getByText("Up to date (version 1)")).toBeVisible();
      if (info.project.name === "phone") {
        await page.setViewportSize({ width: 320, height: 568 });
        await noHorizontalScroll(page);
        await touchTargets(page);
      }
      await axe(page);
    });
  }
});
