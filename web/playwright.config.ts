import { defineConfig, devices } from "@playwright/test";

// The end-to-end smoke run (web-shell.md decision 22, "Test plan"). It runs against a real
// `bms serve` with the hello module and the production build: scripts/e2e.sh (app.md) starts
// the server and sets BMS_E2E_URL. Every scenario runs in two projects: a phone (touch, taps)
// and a desktop (mouse and keyboard).
//
// BMS_E2E_MODE selects the server the run is pointed at:
//   default          hello enabled, writable database
//   hello-disabled   started with BMS_MODULES_DISABLED=hello
//   readonly         database set to default_transaction_read_only = on
export default defineConfig({
  testDir: "./e2e",
  fullyParallel: true,
  forbidOnly: !!process.env["CI"],
  retries: process.env["CI"] ? 1 : 0,
  reporter: process.env["CI"] ? [["list"], ["html", { open: "never" }]] : "list",
  use: {
    baseURL: process.env["BMS_E2E_URL"] ?? "http://localhost:8080",
    trace: "retain-on-failure",
    locale: "en-US",
  },
  projects: [
    {
      name: "phone",
      use: {
        ...devices["Pixel 7"],
        // About 390 x 850 CSS px, touch, mobile: a current mid-range Android phone.
        viewport: { width: 390, height: 850 },
        hasTouch: true,
        isMobile: true,
      },
    },
    {
      name: "desktop",
      use: {
        ...devices["Desktop Chrome"],
        viewport: { width: 1280, height: 800 },
        hasTouch: false,
      },
    },
  ],
});
