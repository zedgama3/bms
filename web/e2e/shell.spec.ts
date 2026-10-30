// End-to-end smoke run, the frame, usage counts, hello, the read-only banner, accessibility and the phone layout, /admin/modules and the wide navigation bar (web-shell.md decision 22, "Test plan").
import { expect } from "@playwright/test";
import {
  watchCsp,
  noHorizontalScroll,
  axe,
  touchTargets,
  openMenu,
  signedOut,
  mockMe,
  test,
  only,
} from "./support/helpers";

test.describe("the frame", () => {
  test.use({ stubPublicSettings: false }); // the real server answers
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
    for (const path of ["/hello", "/sign-in", "/accounts/sign-up", "/admin/modules", "/nowhere"]) {
      await page.goto(path);
      await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    }
    expect(csp).toEqual([]);
  });
});

test.describe("usage counts (D54)", () => {
  test("one navigation produces a screen_view and a screen_ready, sent when the page is hidden", async ({
    page,
  }) => {
    // The browser may ask not to be counted; the collector is then (correctly) not installed.
    const off = await page.evaluate(
      () =>
        (navigator as Navigator & { globalPrivacyControl?: boolean }).globalPrivacyControl ===
          true || navigator.doNotTrack === "1",
    );
    test.skip(off, "the browser asks not to be counted");
    await page.route("**/api/client-events", (r) => r.fulfill({ status: 202 }));
    await page.goto("/");
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    await page.goto("/hello"); // a navigation within the app would do; this is the simplest
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    const sent = page.waitForRequest(
      (r) => new URL(r.url()).pathname === "/api/client-events" && r.method() === "POST",
    );
    // Give the router time to settle (50 ms) and the screen to become ready, then hide the page.
    await page.waitForTimeout(400);
    await page.evaluate(() => window.dispatchEvent(new Event("pagehide")));
    const body = (await sent).postDataJSON() as { events: { kind: string; screen: string }[] };
    const kinds = body.events.map((e) => e.kind);
    expect(kinds).toContain("screen_view");
    expect(kinds).toContain("screen_ready");
    expect(JSON.stringify(body)).not.toMatch(/\?|person|@/);
  });
});

test.describe("hello", () => {
  test.use({ stubPublicSettings: false }); // the real server answers
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

  test("a 401 lands on /accounts/sign-in?returnTo=/hello and shows the sign-in form", async ({
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
    await expect(page).toHaveURL(/\/accounts\/sign-in\?returnTo=%2Fhello$/);
    await expect(page.getByRole("heading", { level: 1, name: "Sign in" })).toBeVisible();
    await expect(page.getByLabel(/E-mail address/)).toBeVisible();
  });
});

test.describe("read-only copy", () => {
  test.use({ stubPublicSettings: false }); // the real server answers
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
  ["sign-up", "/accounts/sign-up"],
  ["hello", "/hello"],
  ["not found", "/nowhere"],
];

test.describe("accessibility and the phone layout", () => {
  test.use({ stubPublicSettings: false }); // the real server answers
  for (const scheme of ["light", "dark"] as const) {
    for (const [name, path] of screens) {
      test(`${name} has no axe violations (${scheme})`, async ({ page }) => {
        only("default");
        await page.emulateMedia({ colorScheme: scheme });
        await signedOut(page);
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
      await signedOut(page);
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
      await signedOut(page);
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
      await mockMe(page);
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

const LONG_NAME =
  "Sparkle Pony of the Deep Playa Who Brings Pickles to Every Burn Since Two Thousand Nine";

test.describe("the wide navigation bar", () => {
  test("a long list of entries wraps whole items, never inside a word", async ({ page }, info) => {
    only("default");
    test.skip(info.project.name !== "desktop", "the horizontal bar is the wide layout");
    const permissions = [
      "core.modules.read",
      "accounts.read",
      "audit.search",
      "jobs.read",
      "settings.read",
      "events.manage",
      "access.roles.read",
      "access.grants.read",
      "people.directory.read",
      "people.data.delete",
      "notify.templates.manage",
      "notify.outbox.manage",
    ];
    await page.route("**/api/accounts/session", (r) =>
      r.fulfill({
        json: {
          signed_in: true,
          person_id: "7d2c2f0e-0000-4000-8000-000000000001",
          playa_name: LONG_NAME,
          superuser: false,
          permissions,
        },
      }),
    );
    await mockMe(page, LONG_NAME);
    for (const width of [768, 1024, 1280]) {
      await page.setViewportSize({ width, height: 860 });
      await page.goto("/");
      const links = page.locator("header nav a");
      await expect(links.first()).toBeVisible();
      expect(await links.count()).toBeGreaterThanOrEqual(10);
      const heights = await links.evaluateAll((els) =>
        els.map((e) => ({
          text: e.textContent ?? "",
          // one line of text: the link is its 44 px minimum, not taller
          height: Math.round(e.getBoundingClientRect().height),
        })),
      );
      for (const h of heights) expect(h.height, `${h.text} at ${width}px`).toBeLessThanOrEqual(48);
      const person = await page
        .getByRole("button", { name: "Sign out" })
        .evaluate((b) => Math.round(b.getBoundingClientRect().height));
      expect(person).toBeLessThanOrEqual(48);
      await noHorizontalScroll(page);
      // a focused element is not hidden under the sticky header (2.4.11)
      const { pad, header } = await page.evaluate(() => ({
        pad: parseFloat(getComputedStyle(document.documentElement).scrollPaddingTop),
        header: document.querySelector("header")?.getBoundingClientRect().height ?? 0,
      }));
      expect(pad, `scroll-padding-top at ${width}px`).toBeGreaterThanOrEqual(header);
    }
  });
});
