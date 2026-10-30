// End-to-end smoke run, the audit log screen (web-shell.md decision 22, "Test plan").
import { expect } from "@playwright/test";
import {
  noHorizontalScroll,
  axe,
  touchTargets,
  mockMe,
  press,
  test,
  only,
  me,
  PAT,
} from "./support/helpers";

// ---- audit (audit.md "UI"): the server is mocked at the network ----

test.describe("audit: the log screen", () => {
  for (const scheme of ["light", "dark"] as const) {
    test(`filters, entries and export lay out at phone width, 44 px targets, axe clean (${scheme})`, async ({
      page,
    }, info) => {
      only("default");
      const isPhone = info.project.name === "phone";
      await page.emulateMedia({ colorScheme: scheme });
      await page.route("**/api/accounts/session", (r) =>
        r.fulfill({ json: { ...me, permissions: ["audit.search", "audit.export"] } }),
      );
      await mockMe(page);
      await page.route("**/api/people/names?**", (r) =>
        r.fulfill({ json: { names: { [PAT]: "Pat Dust" } } }),
      );
      await page.route(/\/api\/audit\/entries(\?.*)?$/, (r) =>
        r.fulfill({
          json: {
            records: [
              {
                v: 1,
                id: "0b6f1c3e-0000-4000-8000-0000000000f1",
                at: "2026-10-05T10:00:00Z",
                instance: "i",
                action: "people.person.read",
                outcome: "succeeded",
                actor: { kind: "person", person: PAT, titles: ["Access administrator"] },
                subject: PAT,
                level: "personal",
                fields: ["phone", "legal_name"],
                record: { kind: "people.person", id: PAT },
              },
            ],
          },
        }),
      );
      await page.route(`**/api/audit/people/${PAT}/views`, (r) =>
        r.fulfill({ contentType: "application/x-ndjson", body: '{"id":"a"}\n{"id":"b"}\n' }),
      );
      if (isPhone) await page.setViewportSize({ width: 320, height: 568 });
      await page.goto(`/audit?subject=${PAT}`);
      await expect(page.getByRole("heading", { level: 1, name: "Audit log" })).toBeVisible();
      await expect(page.getByText("people.person.read", { exact: true })).toBeVisible();
      if (isPhone) {
        await noHorizontalScroll(page);
        await touchTargets(page);
      }
      await axe(page);
      const download = page.waitForEvent("download");
      await press(page.getByRole("button", { name: "Export views" }), isPhone);
      expect((await download).suggestedFilename()).toBe(`views-${PAT}.jsonl`);
    });
  }
});
