import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { safeReturnTo } from "./boot";
import { expectNoAxeViolations } from "@/test/axe";
import { mountApp } from "@/test/app";
import { json, problem } from "@/test/fakeFetch";
import { getSession } from "./session";

const ident = {
  person_id: "7d2c2f0e-0000-4000-8000-000000000001",
  account_id: "0b6f1c3e-0000-4000-8000-000000000002",
  playa_name: "Dusty",
  superuser: false,
  password_change_required: false,
  expires_at: "2026-11-17T10:00:00Z",
  signup_open: true,
  permissions: [],
};

describe("safeReturnTo (F34)", () => {
  it.each([
    ["/hello", "/hello"],
    ["/people/42?tab=1", "/people/42?tab=1"],
    ["//evil.example", "/"],
    ["https://evil.example/x", "/"],
    ["/\\evil.example", "/"],
    ["javascript:alert(1)", "/"],
    ["", "/"],
    [null, "/"],
    ["/x\ny", "/"],
  ])("%j -> %s", (input, want) => {
    expect(safeReturnTo(input)).toBe(want);
  });
});

describe("the session store", () => {
  it("holds the account, expiry, restriction and sign-up flags from GET /session", async () => {
    mountApp({ session: ident });
    await waitFor(() => expect(getSession().signedIn).toBe(true));
    expect(getSession()).toEqual({
      signedIn: true,
      personId: ident.person_id,
      accountId: ident.account_id,
      playaName: "Dusty",
      superuser: false,
      passwordChangeRequired: false,
      expiresAt: new Date("2026-11-17T10:00:00Z"),
      signupOpen: true,
      permissions: [],
    });
  });

  it("signed out it still knows whether sign-up is open", async () => {
    mountApp({
      routes: { "GET /api/accounts/session": () => json({ signed_in: false, signup_open: true }) },
    });
    await waitFor(() => expect(getSession()).toEqual({ signedIn: false, signupOpen: true }));
  });
});

describe("/sign-in (an alias since M1)", () => {
  it("redirects to /accounts/sign-in and keeps the query", async () => {
    const { router } = mountApp({
      path: "/sign-in?returnTo=%2Fhello",
      routes: { "GET /api/accounts/session": () => json({ signed_in: false }) },
    });
    await waitFor(() => expect(router()!.state.location.pathname).toBe("/accounts/sign-in"));
    expect(router()!.state.location.search).toBe("?returnTo=%2Fhello");
    expect(await screen.findByRole("heading", { level: 1, name: "Sign in" })).toBeInTheDocument();
  });

  it("a signed-in person visiting sign-in is sent on", async () => {
    const { router } = mountApp({
      path: "/accounts/sign-in?returnTo=%2Fhello",
      session: ident,
    });
    await waitFor(() => expect(router()!.state.location.pathname).toBe("/hello"));
  });

  it("a 401 elsewhere clears the session and cache, goes to sign-in with returnTo, and toasts", async () => {
    const { router, client } = mountApp({
      path: "/admin/modules",
      session: ident,
      routes: { "GET /api/admin/modules": () => problem(401, { code: "unauthenticated" }) },
    });
    await waitFor(() => expect(router()!.state.location.pathname).toBe("/accounts/sign-in"));
    expect(router()!.state.location.search).toBe("?returnTo=%2Fadmin%2Fmodules");
    // signed out, but still knowing sign-up is open so the sign-in screen can offer it
    expect(getSession()).toEqual({ signedIn: false, signupOpen: true });
    expect(await screen.findByText("Your session ended. Sign in again.")).toBeInTheDocument();
    expect(client.getQueryCache().find({ queryKey: ["core", "admin-modules"] })).toBeUndefined();
    await act(async () => {});
  });
});

describe("the frame for accounts", () => {
  it("shows a neutral label when there is no playa name", async () => {
    mountApp({ session: { ...ident, playa_name: "" } });
    const header = await screen.findByRole("banner");
    expect(await within(header).findByText("Account", { selector: "span" })).toBeInTheDocument();
    expect(within(header).queryByText("Dusty")).not.toBeInTheDocument();
  });

  it("sends a restricted session to /accounts/password?returnTo= from any other route", async () => {
    const { router, container } = mountApp({
      path: "/hello",
      session: { ...ident, password_change_required: true },
    });
    await waitFor(() => expect(router()!.state.location.pathname).toBe("/accounts/password"));
    expect(router()!.state.location.search).toBe("?returnTo=%2Fhello");
    expect(
      await screen.findByText("You must choose a new password before you continue."),
    ).toBeInTheDocument();
    // the navigation offers nothing else to a restricted session
    await userEvent.click(screen.getByRole("button", { name: "Menu" }));
    const nav = screen.getByRole("navigation", { name: "Main" });
    expect(
      within(nav)
        .getAllByRole("link")
        .map((l) => l.textContent),
    ).toEqual(["Account"]);
    await expectNoAxeViolations(container);
  });

  it.each(["/accounts/admin", "/accounts/sign-up"])(
    "a restricted session on %s is sent to the password screen",
    async (path) => {
      const { router } = mountApp({
        path,
        session: { ...ident, password_change_required: true },
      });
      await waitFor(() => expect(router()!.state.location.pathname).toBe("/accounts/password"));
      expect(router()!.state.location.search).toBe(`?returnTo=${encodeURIComponent(path)}`);
    },
  );

  it("stays on the reset screen for a restricted session, with no returnTo", async () => {
    const { router } = mountApp({
      path: "/accounts/password-reset?token=abc",
      session: { ...ident, password_change_required: true },
    });
    await waitFor(() => expect(router()!.state.location.pathname).toBe("/accounts/password-reset"));
    expect(router()!.state.location.search).not.toContain("returnTo");
  });

  it("does not redirect an ordinary session", async () => {
    const { router } = mountApp({ path: "/hello", session: ident });
    await screen.findByRole("heading", { level: 1, name: "Hello!" });
    expect(router()!.state.location.pathname).toBe("/hello");
  });
});
