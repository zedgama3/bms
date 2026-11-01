// End-to-end smoke run, the settings screens, terminology, read-only copy and the logo (web-shell.md decision 22, "Test plan").
import { deflateSync } from "node:zlib";
import { expect } from "@playwright/test";
import type { Page } from "@playwright/test";
import {
  publicSettings,
  mockPublicSettings,
  noHorizontalScroll,
  axe,
  touchTargets,
  openMenu,
  mockMe,
  press,
  test,
  only,
  adminSession,
} from "./support/helpers";

// ---- settings (M4): the regional's values, terminology and an area's form, mocked at the network ----

/** An in-memory settings document behind the settings routes. The public document follows what
 *  was saved, as the real one does. */
async function mockSettings(page: Page, permissions: string[]) {
  await page.route("**/api/accounts/session", (r) =>
    r.fulfill({ json: adminSession(permissions) }),
  );
  await mockMe(page);
  const state = {
    name: "BMS",
    color: "",
    departments: "Departments",
    contrastError: true,
    saved: [] as unknown[],
    termSaves: [] as unknown[],
  };
  const text = (key: string, over: Record<string, unknown> = {}) => ({
    key,
    kind: "text",
    default: "",
    value: null,
    secret_set: false,
    stale: false,
    ...over,
  });
  await mockPublicSettings(page, () =>
    publicSettings({
      name: state.name,
      terms: {
        en: {
          term_department: state.departments.toLowerCase(),
          term_department_plural: state.departments.toLowerCase(),
          term_department_title: state.departments,
          term_department_plural_title: state.departments,
        },
      },
    }),
  );
  await page.route(/\/api\/settings\/?$/, (r) =>
    r.fulfill({
      json: {
        revision: 3,
        areas: [
          {
            name: "regional",
            core: true,
            settings: [
              text("regional.name", {
                default: "BMS",
                value: state.name === "BMS" ? null : state.name,
              }),
              text("regional.contact_email"),
              text("regional.public_url"),
              text("regional.language", { kind: "enum", default: "en", options: ["en", "es"] }),
              text("regional.timezone"),
              text("regional.color_primary", { value: state.color || null }),
              text("regional.color_primary_dark"),
            ],
          },
          {
            name: "accounts",
            core: true,
            settings: [
              text("accounts.session_lifetime", { kind: "duration", default: "720h" }),
              text("accounts.password_min_length", { kind: "int", default: "12" }),
            ],
          },
          {
            name: "hello",
            core: false,
            settings: [
              text("hello.greeting", { default: "Hello" }),
              text("hello.token", { kind: "secret", secret_set: true }),
            ],
          },
        ],
      },
    }),
  );
  await page.route("**/api/settings/values", (r) => {
    const body = r.request().postDataJSON() as { values: Record<string, string | null> };
    state.saved.push(body);
    const color = body.values["regional.color_primary"];
    if (color && state.contrastError) {
      state.contrastError = false;
      return r.fulfill({
        status: 400,
        contentType: "application/problem+json",
        body: JSON.stringify({
          status: 400,
          code: "invalid",
          fields: [
            {
              field: "regional.color_primary",
              code: "contrast",
              reason: "too faint",
              params: { ratio: "1.6", min: "4.5" },
            },
          ],
        }),
      });
    }
    if (body.values["regional.name"]) state.name = body.values["regional.name"];
    if (color) state.color = color;
    return r.fulfill({ json: { revision: 4 } });
  });
  const form = (singular: string, plural: string, st: string, pt: string) => ({
    singular,
    plural,
    singular_title: st,
    plural_title: pt,
  });
  await page.route("**/api/settings/terms", (r) =>
    r.fulfill({
      json: {
        languages: ["en"],
        terms: {
          en: {
            department: {
              default: form("department", "departments", "Department", "Departments"),
              override:
                state.departments === "Departments" ? {} : { plural_title: state.departments },
            },
            event: {
              default: form("event", "events", "Event", "Events"),
              override: {},
            },
          },
        },
      },
    }),
  );
  await page.route("**/api/settings/terms/en", (r) => {
    const body = r.request().postDataJSON() as {
      terms: Record<string, { plural_title?: string }>;
    };
    state.termSaves.push(body);
    state.departments = body.terms["department"]?.plural_title ?? state.departments;
    return r.fulfill({ json: { revision: 5 } });
  });
  return { state };
}

test.describe("settings: the M4 screens", () => {
  for (const scheme of ["light", "dark"] as const) {
    test(`an admin changes the name and colour, fixes a contrast error, and renames Departments, with touch alone (${scheme})`, async ({
      page,
    }, info) => {
      only("default");
      const isPhone = info.project.name === "phone";
      await page.emulateMedia({ colorScheme: scheme });
      const { state } = await mockSettings(page, ["settings.read", "settings.manage"]);
      if (isPhone) await page.setViewportSize({ width: 320, height: 568 });
      const check = async () => {
        if (isPhone) {
          await noHorizontalScroll(page);
          await touchTargets(page);
        }
        await axe(page);
      };

      await page.goto("/settings");
      await expect(page.getByRole("heading", { level: 1, name: "Settings" })).toBeVisible();
      await expect(page.getByRole("link", { name: "BMS", exact: true }).first()).toBeVisible();
      await expect(page.getByLabel("Contact mailbox")).toHaveAttribute("type", "email");
      await expect(page.getByLabel("Web address")).toHaveAttribute("type", "url");
      await check();

      // a colour that is too faint: said live, refused by the server, the edits are kept
      await page.getByLabel(/^Name/).fill("Playa Hall");
      await page.getByLabel("Brand colour", { exact: true }).fill("#cccccc");
      await expect(page.getByText(/too low \(at least 4\.5 to 1 is needed\)/)).toBeVisible();
      await press(page.getByRole("button", { name: "Save" }), isPhone);
      const alert = page.getByRole("alert");
      await expect(alert).toContainText("Brand colour");
      await expect(alert).toContainText("1.6 to 1");
      await expect(page.getByLabel("Brand colour", { exact: true })).toHaveAttribute(
        "aria-invalid",
        "true",
      );
      await expect(page.getByLabel(/^Name/)).toHaveValue("Playa Hall");
      await check();

      await page.getByLabel("Brand colour", { exact: true }).fill("#0b5cad");
      await press(page.getByRole("button", { name: "Save" }), isPhone);
      await expect(page.getByText("Saved")).toBeVisible();
      // the header follows without a reload
      await expect(
        page.getByRole("banner").getByRole("link", { name: "Playa Hall" }),
      ).toBeVisible();
      expect(state.saved.at(-1)).toEqual({
        values: { "regional.name": "Playa Hall", "regional.color_primary": "#0b5cad" },
      });

      await press(page.getByRole("link", { name: "Terminology" }), isPhone);
      await expect(page).toHaveURL(/\/settings\/terms$/);
      const group = page.getByRole("group", { name: "Department" });
      await group.getByLabel("Several, as a title").fill("Teams");
      await check();
      await press(page.getByRole("button", { name: "Save English" }), isPhone);
      // toasts stack: the name save's may still be up, so wait for the terms save itself,
      // then look at the newest toast
      await expect.poll(() => state.termSaves.length).toBe(1);
      await expect(page.getByText("Saved").last()).toBeVisible();
      expect(state.termSaves).toEqual([{ terms: { department: { plural_title: "Teams" } } }]);
      const nav = await openMenu(page, isPhone);
      await expect(nav.getByRole("link", { name: "Teams" })).toBeVisible();
    });
  }

  test("an area form shows a duration as a number and unit and never shows a secret", async ({
    page,
  }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    const { state } = await mockSettings(page, ["settings.read", "settings.manage"]);
    if (isPhone) await page.setViewportSize({ width: 320, height: 568 });

    await page.goto("/settings");
    await press(page.getByRole("link", { name: "Accounts" }), isPhone);
    await expect(page).toHaveURL(/\/settings\/accounts$/);
    await expect(page.getByLabel("How long people stay signed in", { exact: true })).toHaveValue(
      "30",
    );
    await expect(page.getByLabel("Unit for How long people stay signed in")).toHaveValue("d");
    await page.getByLabel("How long people stay signed in", { exact: true }).fill("14");
    await press(page.getByRole("button", { name: "Save" }), isPhone);
    await expect(page.getByText("Saved")).toBeVisible();
    expect(state.saved.at(-1)).toEqual({ values: { "accounts.session_lifetime": "336h" } });

    await page.goto("/settings/hello");
    await expect(page.getByText("A value is set. It can't be shown.")).toBeVisible();
    await expect(page.locator('input[type="password"]')).toHaveCount(0);
    await press(page.getByRole("button", { name: "Replace: hello.token" }), isPhone);
    await expect(page.getByLabel("New value for hello.token")).toHaveAttribute("type", "password");
    if (isPhone) {
      await noHorizontalScroll(page);
      await touchTargets(page);
    }
    await axe(page);
  });

  test("a participant cannot open /settings and has no menu entry for it", async ({
    page,
  }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    await mockSettings(page, []);
    await page.goto("/settings");
    await expect(page.getByText("You don't have access to this screen.")).toBeVisible();
    await expect(page.getByLabel(/^Name/)).toHaveCount(0);
    if (isPhone) await page.getByRole("button", { name: "Menu" }).tap();
    await expect(page.getByRole("link", { name: "Settings", exact: true })).toHaveCount(0);
    await axe(page);
  });

  test("the departments page title follows a term rename", async ({ page }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    await mockSettings(page, ["settings.read", "settings.manage"]);
    await page.route(/\/api\/departments\/?(\?.*)?$/, (r) =>
      r.fulfill({ json: { departments: [] } }),
    );
    await page.goto("/settings/terms");
    await page
      .getByRole("group", { name: "Department" })
      .getByLabel("Several, as a title")
      .fill("Teams");
    await press(page.getByRole("button", { name: "Save English" }), isPhone);
    await expect(page.getByText("Saved")).toBeVisible();
    await page.goto("/departments");
    await expect(page.getByRole("heading", { level: 1, name: "Teams" })).toBeVisible();
    await expect(page).toHaveTitle(/^Teams · /);
  });

  test("a person who can manage but not read gets an explanation and no form", async ({ page }) => {
    only("default");
    await mockSettings(page, ["settings.manage"]);
    await page.goto("/settings");
    await expect(page.getByText(/change settings but not read them/)).toBeVisible();
    await expect(page.getByLabel(/^Name/)).toHaveCount(0);
    await axe(page);
  });
});

test.describe("read-only copy: settings", () => {
  test("Save is disabled with the reason and the form keeps its edits", async ({ page }) => {
    only("readonly");
    await mockSettings(page, ["settings.read", "settings.manage"]);
    const readOnly = { "X-Read-Only": "true" };
    await page.route(/\/api\/settings\/?$/, (r) =>
      r.fulfill({
        headers: readOnly,
        json: {
          revision: 1,
          areas: [
            {
              name: "regional",
              core: true,
              settings: [
                {
                  key: "regional.name",
                  kind: "text",
                  default: "BMS",
                  value: null,
                  secret_set: false,
                  stale: false,
                },
              ],
            },
          ],
        },
      }),
    );
    await page.goto("/settings");
    await page.getByLabel(/^Name/).fill("Playa Hall");
    await expect(page.getByRole("button", { name: "Save" })).toBeDisabled();
    await expect(
      page.getByText("This copy is read-only, so this can't be saved right now."),
    ).toBeVisible();
    await expect(page.getByLabel(/^Name/)).toHaveValue("Playa Hall");
  });
});

// ---- settings: the logo (D47), mocked at the network; a generated 1x1 PNG, no real image ----

/** A 1x1 transparent PNG, built from its bytes so no image file sits in the repository. */
const TINY_PNG = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==",
  "base64",
);

/** A solid w x h grey PNG built in memory (a wide one for the header's layout check). */
function solidPng(w: number, h: number): Buffer {
  const crcTable = Array.from({ length: 256 }, (_, n) => {
    let c = n;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    return c >>> 0;
  });
  const crc = (b: Buffer) => {
    let c = 0xffffffff;
    for (const x of b) c = crcTable[(c ^ x) & 0xff]! ^ (c >>> 8);
    return (c ^ 0xffffffff) >>> 0;
  };
  const chunk = (type: string, data: Buffer) => {
    const body = Buffer.concat([Buffer.from(type, "ascii"), data]);
    const len = Buffer.alloc(4);
    len.writeUInt32BE(data.length);
    const sum = Buffer.alloc(4);
    sum.writeUInt32BE(crc(body));
    return Buffer.concat([len, body, sum]);
  };
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(w, 0);
  ihdr.writeUInt32BE(h, 4);
  ihdr[8] = 8; // bit depth
  ihdr[9] = 2; // RGB
  const row = Buffer.concat([Buffer.from([0]), Buffer.alloc(w * 3, 0x80)]);
  const raw = Buffer.concat(Array.from({ length: h }, () => row));
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk("IHDR", ihdr),
    chunk("IDAT", deflateSync(raw)),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}

test.describe("settings: the logo", () => {
  test("an admin uploads a logo, sees it in the header, and removes it behind a confirmation, with touch alone", async ({
    page,
  }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    await mockSettings(page, ["settings.read", "settings.manage"]);
    const logo = { id: "", puts: 0, deletes: 0 };
    // Later routes win: the public document now carries the logo's file ID.
    await mockPublicSettings(page, () => publicSettings({ name: "BMS", logo: logo.id }));
    await page.route("**/api/regional/logo**", (r) => {
      const method = r.request().method();
      if (method === "PUT") {
        logo.puts++;
        logo.id = `file-${logo.puts}`;
        return r.fulfill({ json: { revision: 6, logo: logo.id } });
      }
      if (method === "DELETE") {
        logo.deletes++;
        logo.id = "";
        return r.fulfill({ json: { revision: 7, logo: "" } });
      }
      return r.fulfill({ contentType: "image/png", body: TINY_PNG });
    });
    if (isPhone) await page.setViewportSize({ width: 320, height: 568 });
    const check = async () => {
      if (isPhone) {
        await noHorizontalScroll(page);
        await touchTargets(page);
      }
      await axe(page);
    };

    await page.goto("/settings");
    const section = page.getByRole("region", { name: "Logo" });
    await expect(
      section.getByText(/No logo yet\. The site shows its name, BMS, as text\./),
    ).toBeVisible();
    await expect(section.getByRole("button", { name: "Upload" })).toBeDisabled();
    await check();

    // choosing a file changes nothing until Upload is pressed
    await section
      .getByLabel("Logo", { exact: true })
      .setInputFiles({ name: "logo.png", mimeType: "image/png", buffer: TINY_PNG });
    await expect(section.getByText(/Chosen file: logo\.png/)).toBeVisible();
    expect(logo.puts).toBe(0);
    await press(section.getByRole("button", { name: "Upload" }), isPhone);
    await expect(page.getByText("Logo updated")).toBeVisible();
    expect(logo.puts).toBe(1);
    await expect(section.getByRole("img", { name: "The current logo" })).toHaveAttribute(
      "src",
      "/api/regional/logo?v=file-1",
    );
    const home = page.getByRole("banner").getByRole("link", { name: "BMS", exact: true });
    await expect(home.getByRole("img", { name: "BMS" })).toBeVisible();
    await expect(section.getByRole("button", { name: "Replace" })).toBeDisabled();
    await check();

    // remove: Cancel first, then confirm
    await press(section.getByRole("button", { name: "Remove logo" }), isPhone);
    const dialog = page.getByRole("dialog", { name: "Remove logo" });
    await expect(dialog).toBeVisible();
    await press(dialog.getByRole("button", { name: "Cancel" }), isPhone);
    await expect(dialog).toBeHidden();
    expect(logo.deletes).toBe(0);
    await expect(section.getByRole("button", { name: "Remove logo" })).toBeFocused();
    await press(section.getByRole("button", { name: "Remove logo" }), isPhone);
    await press(dialog.getByRole("button", { name: "Remove", exact: true }), isPhone);
    await expect(page.getByText("Logo removed")).toBeVisible();
    expect(logo.deletes).toBe(1);
    await expect(section.getByText(/No logo yet/)).toBeVisible();
    await expect(home.getByRole("img")).toHaveCount(0);
    await check();
  });

  test("a file the server refuses is explained on the input and the file is kept", async ({
    page,
  }, info) => {
    only("default");
    const isPhone = info.project.name === "phone";
    await mockSettings(page, ["settings.read", "settings.manage"]);
    await page.route("**/api/regional/logo**", (r) =>
      r.fulfill({
        status: 400,
        contentType: "application/problem+json",
        body: JSON.stringify({
          status: 400,
          code: "invalid",
          fields: [{ field: "file", code: "type", reason: "not an image" }],
        }),
      }),
    );
    await page.goto("/settings");
    const section = page.getByRole("region", { name: "Logo" });
    await section
      .getByLabel("Logo", { exact: true })
      .setInputFiles({ name: "logo.png", mimeType: "image/png", buffer: TINY_PNG });
    await press(section.getByRole("button", { name: "Upload" }), isPhone);
    await expect(page.getByRole("alert")).toContainText("This kind of file isn't accepted.");
    await expect(section.getByLabel("Logo", { exact: true })).toHaveAttribute(
      "aria-invalid",
      "true",
    );
    await expect(section.getByText(/Chosen file: logo\.png/)).toBeVisible();
    await axe(page);
  });

  test("a wide logo keeps the header on one screen width, with the Menu button in view", async ({
    page,
  }, info) => {
    const isPhone = info.project.name === "phone";
    only("default");
    if (!isPhone) test.skip(true, "phone layout");
    const wide = solidPng(600, 100);
    await mockSettings(page, ["settings.read", "settings.manage"]);
    await mockPublicSettings(page, () => publicSettings({ name: "BMS", logo: "wide-1" }));
    await page.route("**/api/regional/logo**", (r) =>
      r.fulfill({ contentType: "image/png", body: wide }),
    );
    await page.setViewportSize({ width: 320, height: 568 });
    await page.goto("/settings");
    const home = page.getByRole("link", { name: "BMS" }).first();
    const img = home.getByRole("img");
    await expect(img).toBeVisible();
    await expect.poll(() => img.evaluate((el: HTMLImageElement) => el.naturalWidth)).toBe(600);
    const menu = page.getByRole("button", { name: "Menu" });
    await expect(menu).toBeVisible();
    await expect(menu).toBeInViewport({ ratio: 1 });
    await noHorizontalScroll(page);
    const box = await img.boundingBox();
    expect(box?.width ?? 0).toBeLessThanOrEqual(192.5);
    expect(box?.height ?? 0).toBeLessThanOrEqual(32.5);
    await touchTargets(page);
    await axe(page);
  });
});
