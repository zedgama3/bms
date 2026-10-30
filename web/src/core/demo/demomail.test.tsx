import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { demoStore, DEMO_OFF } from "@/api/internal";
import { expectNoAxeViolations } from "@/test/axe";
import { mountApp } from "@/test/app";
import { json, problem } from "@/test/fakeFetch";

const ident = {
  person_id: "7d2c2f0e-0000-4000-8000-000000000001",
  account_id: "0b6f1c3e-0000-4000-8000-000000000002",
  playa_name: "Dusty",
  superuser: false,
  password_change_required: false,
  expires_at: "2026-11-17T10:00:00Z",
  signup_open: false,
  permissions: [],
};

const MAIL = [
  {
    id: "11111111-0000-4000-8000-000000000001",
    to: "participant@demo.invalid",
    subject: "Reset your password",
    body: "Hello,\nOpen https://demo.example/accounts/password-reset?token=abc123.\nThanks",
    captured_at: new Date(Date.now() - 5 * 60_000).toISOString(),
  },
  {
    id: "11111111-0000-4000-8000-000000000002",
    to: "participant@demo.invalid",
    subject: "Welcome",
    body: "Welcome aboard",
    captured_at: new Date(Date.now() - 3 * 3600_000).toISOString(),
  },
];

afterEach(() => demoStore.set(DEMO_OFF));

const mount = (mail: (req: Request) => Response | Promise<Response>, signedIn = true) =>
  mountApp({
    path: "/demo/mail",
    ...(signedIn ? { session: ident } : {}),
    routes: { "GET /api/demo/mail": mail },
  });

describe("/demo/mail (F43)", () => {
  it("lists the account's own mail, newest first, and opens a body on tap", async () => {
    const seen: string[] = [];
    const { container } = mount((req) => {
      seen.push(new URL(req.url).search);
      return json({ mail: MAIL });
    });
    expect(
      await screen.findByRole("heading", { level: 1, name: "Captured mail" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/Only mail sent to your own account's address appears here/),
    ).toBeInTheDocument();
    const first = await screen.findByRole("button", { name: /Reset your password/ });
    expect(first).toHaveAttribute("aria-expanded", "false");
    await userEvent.click(first);
    expect(first).toHaveAttribute("aria-expanded", "true");
    const link = screen.getByRole("link", {
      name: "https://demo.example/accounts/password-reset?token=abc123",
    });
    expect(link).toHaveAttribute(
      "href",
      "https://demo.example/accounts/password-reset?token=abc123",
    );
    await expectNoAxeViolations(container);
    // only the signed-in account's own mail is asked for: no id, address or other parameter
    expect(seen.every((s) => s === "")).toBe(true);
  });

  it("empty", async () => {
    const { container } = mount(() => json({ mail: [] }));
    expect(await screen.findByText(/No mail has been captured yet/)).toBeInTheDocument();
    await expectNoAxeViolations(container);
  });

  it("loading", async () => {
    mount(() => new Promise<Response>(() => {}));
    expect(
      await screen.findByRole("heading", { level: 1, name: "Captured mail" }),
    ).toBeInTheDocument();
    expect(screen.queryByText(/No mail has been captured yet/)).not.toBeInTheDocument();
    expect(document.querySelector("[aria-busy=true]")).not.toBeNull();
  });

  it("signed out: goes to sign-in", async () => {
    const calls = vi.fn();
    mount(() => (calls(), json({ mail: [] })), false);
    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
    expect(calls).not.toHaveBeenCalled();
  });

  it("a 401 sends to sign-in", async () => {
    mount(() => problem(401, { code: "unauthenticated" }));
    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
  });

  it("off: 501 says this is not a demo instance", async () => {
    mount(() => problem(501, { code: "unsupported" }));
    expect(await screen.findByText("This is not a demo instance.")).toBeInTheDocument();
  });

  it("a 404 (an older server without the route) is also 'not a demo instance'", async () => {
    mount(() => problem(404, { code: "not_found" }));
    expect(await screen.findByText("This is not a demo instance.")).toBeInTheDocument();
  });

  it("error shows the reference and retries", async () => {
    vi.spyOn(console, "debug").mockImplementation(() => {});
    let fail = true;
    mount(() =>
      fail ? problem(500, { code: "internal", ref: "AB12-CD34" }) : json({ mail: MAIL }),
    );
    expect(await screen.findByText("AB12-CD34")).toBeInTheDocument();
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: /Welcome/ })).toBeInTheDocument(),
    );
  });
});
