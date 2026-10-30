// End-to-end smoke run, the e-mail admin screens (web-shell.md decision 22, "Test plan").
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

test.describe("notify: the e-mail admin screens", () => {
  const NAME = "identity.password-reset";
  const MSG = "9a1f0000-0000-4000-8000-000000000001";
  const viewOf = (subject: string, body: string, overridden: boolean) => ({
    name: NAME,
    language: "en",
    overridden,
    vars: ["lifetime"],
    default: { subject: "Reset your password", body: "Open {{.Link}} within {{.Vars.lifetime}}." },
    current: { subject, body },
  });

  test("edit a template, preview it, save it, reset it and send a test, with touch alone", async ({
    page,
  }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    if (isPhone) await page.setViewportSize({ width: 320, height: 568 });
    await page.route("**/api/accounts/session", (r) =>
      r.fulfill({ json: adminSession(["notify.templates.manage", "notify.outbox.manage"]) }),
    );
    await mockMe(page);
    const saved: unknown[] = [];
    let overridden = false;
    let text = {
      subject: "Reset your password",
      body: "Open {{.Link}} within {{.Vars.lifetime}}.",
    };
    await page.route("**/api/notify/templates", (r) =>
      r.fulfill({
        json: [
          {
            name: NAME,
            available: true,
            vars: ["lifetime"],
            languages: [{ language: "en", overridden }],
          },
        ],
      }),
    );
    await page.route(`**/api/notify/templates/${NAME}/en`, (r) => {
      const method = r.request().method();
      if (method === "PUT") {
        const body = r.request().postDataJSON() as typeof text;
        saved.push(body);
        text = body;
        overridden = true;
      }
      if (method === "DELETE") {
        overridden = false;
        text = {
          subject: "Reset your password",
          body: "Open {{.Link}} within {{.Vars.lifetime}}.",
        };
        return r.fulfill({ status: 204 });
      }
      return r.fulfill({ json: viewOf(text.subject, text.body, overridden) });
    });
    await page.route(`**/api/notify/templates/${NAME}/en/preview`, (r) =>
      r.fulfill({ json: { subject: "Rendered subject", body: "Rendered body" } }),
    );
    await page.route("**/api/notify/test", (r) => r.fulfill({ status: 202, json: { id: MSG } }));
    const check = async () => {
      if (isPhone) {
        await noHorizontalScroll(page);
        await touchTargets(page);
      }
      await axe(page);
    };

    await page.goto("/notify/templates");
    await expect(page.getByRole("heading", { level: 1, name: "E-mail templates" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Password-reset link" })).toBeVisible();
    await check();
    await press(page.getByRole("button", { name: "Send me a test" }), isPhone);
    await expect(page.getByText(MSG)).toBeVisible();
    await press(page.getByRole("link", { name: "en: shipped text" }), isPhone);
    await expect(page).toHaveURL(/\/notify\/templates\/identity\.password-reset\/en$/);

    await page.getByLabel(/^Subject/).fill("Choose a new password");
    await press(page.getByRole("button", { name: "Preview" }), isPhone);
    await expect(page.getByText("Rendered subject")).toBeVisible();
    await check();
    await press(page.getByRole("button", { name: "Save" }), isPhone);
    await expect(page.getByText("Saved", { exact: true })).toBeVisible();
    expect(saved).toEqual([
      { subject: "Choose a new password", body: "Open {{.Link}} within {{.Vars.lifetime}}." },
    ]);

    await press(page.getByRole("button", { name: "Reset to default" }), isPhone);
    const dialog = page.getByRole("dialog");
    await press(dialog.getByRole("button", { name: "Reset to default" }), isPhone);
    await expect(page.getByLabel(/^Subject/)).toHaveValue("Reset your password");
    await expect(page.getByRole("button", { name: "Reset to default" })).toHaveCount(0);
    await check();
  });

  test("the outbox: counts, retry, cancel and the connection check, with no address on the page", async ({
    page,
  }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    if (isPhone) await page.setViewportSize({ width: 320, height: 568 });
    await page.route("**/api/accounts/session", (r) =>
      r.fulfill({ json: adminSession(["notify.outbox.manage"]) }),
    );
    await mockMe(page);
    const calls: string[] = [];
    await page.route("**/api/notify/outbox", (r) =>
      r.fulfill({
        json: {
          configured: true,
          counts: { queued: 1, sending: 0, sent: 12, failed: 1 },
          recent: [
            {
              id: MSG,
              owner: "identity",
              template: "password-reset",
              language: "en",
              status: "failed",
              attempts: 3,
              created_at: "2026-10-19T10:00:00Z",
              expires_at: "2099-01-01T00:00:00Z",
              last_error: "connect",
            },
          ],
        },
      }),
    );
    await page.route("**/api/people/names?**", (r) => r.fulfill({ json: { names: {} } }));
    await page.route(`**/api/notify/outbox/${MSG}/retry`, (r) => {
      calls.push("retry");
      return r.fulfill({
        status: 409,
        contentType: "application/problem+json",
        body: JSON.stringify({
          status: 409,
          code: "conflict",
          fields: [{ field: "status", code: "expired", reason: "expired" }],
        }),
      });
    });
    await page.route(`**/api/notify/outbox/${MSG}/cancel`, (r) => {
      calls.push("cancel");
      return r.fulfill({ status: 204 });
    });
    await page.route("**/api/notify/outbox/check", (r) => r.fulfill({ status: 204 }));
    const check = async () => {
      if (isPhone) {
        await noHorizontalScroll(page);
        await touchTargets(page);
      }
      await axe(page);
    };

    await page.goto("/notify/outbox");
    await expect(page.getByRole("heading", { level: 1, name: "E-mail outbox" })).toBeVisible();
    await expect(page.getByText(MSG)).toBeVisible();
    await expect(page.getByText("Couldn't reach the mail server", { exact: true })).toBeVisible();
    await expect(page.getByText("Password-reset link", { exact: true })).toBeVisible();
    await expect(page.getByText("An e-mail address (no account)")).toBeVisible();
    expect(await page.locator("main").innerText()).not.toContain("@");
    await check();
    await press(page.getByRole("button", { name: "Check connection" }), isPhone);
    await expect(page.getByText(/mail server answered/)).toBeVisible();
    await press(page.getByRole("button", { name: `Retry message ${MSG}` }), isPhone);
    await expect(page.getByRole("alert")).toContainText("This message has expired");
    await press(page.getByRole("button", { name: `Cancel message ${MSG}` }), isPhone);
    await press(page.getByRole("dialog").getByRole("button", { name: "Cancel message" }), isPhone);
    await expect(page.getByText("Message canceled")).toBeVisible();
    expect(calls).toEqual(["retry", "cancel"]);
    await check();
  });

  test("a participant has no entry and gets the forbidden state", async ({ page }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    await page.route("**/api/accounts/session", (r) => r.fulfill({ json: adminSession([]) }));
    await mockMe(page);
    await page.goto("/notify/outbox");
    await expect(page.getByText("You don't have access to this screen.")).toBeVisible();
    if (isPhone) await page.getByRole("button", { name: "Menu" }).tap();
    await expect(page.getByRole("link", { name: "E-mail outbox" })).toHaveCount(0);
    await axe(page);
  });
});
