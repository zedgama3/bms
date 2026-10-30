import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { demoStore, DEMO_OFF } from "@/api/internal";
import { expectNoAxeViolations } from "@/test/axe";
import { mountApp } from "@/test/app";
import { json, problem } from "@/test/fakeFetch";
import { installReporter, uninstallReporterForTest } from "./report";
import type { Batch } from "./report";

const DEMO = {
  enabled: true,
  regional: "Ember Flats",
  password: "correct-horse-battery",
  accounts: [
    { key: "participant", email: "participant@demo.invalid", roles: [] },
    {
      key: "lead",
      email: "lead@demo.invalid",
      roles: ["department-admin@rangers", "department-admin@gate", "department-admin@dpw"],
    },
    { key: "steward", email: "steward@demo.invalid", roles: ["department-steward"] },
  ],
};

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

afterEach(() => {
  uninstallReporterForTest();
  demoStore.set(DEMO_OFF);
});

describe("demo banner", () => {
  it("F41: renders on every route and expanded on sign-in", async () => {
    const { container, router } = mountApp({
      routes: { "GET /api/demo/": () => json(DEMO) },
    });
    const note = await screen.findByRole("note");
    expect(note).toHaveTextContent("Public demo.");
    expect(note).toHaveTextContent("Everything you enter is visible to strangers");
    expect(note).toHaveTextContent("made-up people only");
    // not expanded on the home screen
    expect(screen.queryByText("Try a demo account")).not.toBeInTheDocument();
    // not signed in: no mail link
    expect(screen.queryByRole("link", { name: "Captured mail" })).not.toBeInTheDocument();
    await expectNoAxeViolations(container);

    await router()!.navigate("/hello");
    await screen.findByRole("heading", { level: 1 });
    expect(screen.getByRole("note")).toBeInTheDocument();

    await router()!.navigate("/accounts/sign-in");
    expect(await screen.findByText("Try a demo account")).toBeInTheDocument();
    expect(
      screen.getByText("Password for every demo account: correct-horse-battery"),
    ).toBeInTheDocument();
    const table = screen.getByRole("table", { name: "Demo accounts" });
    const rows = within(table).getAllByRole("row");
    expect(rows).toHaveLength(1 + DEMO.accounts.length);
    expect(within(table).getByText("lead@demo.invalid")).toBeInTheDocument();
    // an address may break only after the @, never mid-word
    const code = within(table).getByText("lead@demo.invalid");
    expect(code.querySelectorAll("wbr")).toHaveLength(1);
    expect(code.className).not.toMatch(/break-all/);
    expect(code.innerHTML).toMatch(/@<wbr>/);
    // One short label per account (D57): the lead's departments are joined in role order.
    expect(within(table).getByText("Lead: Rangers, Gate, DPW")).toBeInTheDocument();
    expect(within(table).getByText("Participant")).toBeInTheDocument();
    expect(within(table).getByText("Department steward")).toBeInTheDocument();
    expect(within(table).queryByText(/Administrator of a department/)).toBeNull();
    expect(within(table).queryByText(/department-admin/)).toBeNull();
    expect(screen.getAllByRole("note")).toHaveLength(1);
    await expectNoAxeViolations(container);
  });

  it("has no copy button, and a sign-in button per account named by its label", async () => {
    mountApp({ routes: { "GET /api/demo/": () => json(DEMO) }, path: "/accounts/sign-in" });
    const table = await screen.findByRole("table", { name: "Demo accounts" });
    expect(screen.queryByRole("button", { name: /copy password/i })).toBeNull();
    expect(
      screen.getByText("Password for every demo account: correct-horse-battery"),
    ).toBeInTheDocument();
    const names = within(table)
      .getAllByRole("button")
      .map((b) => b.getAttribute("aria-label") ?? b.textContent);
    expect(names).toEqual([
      "Sign in as Participant",
      "Sign in as Lead: Rangers, Gate, DPW",
      "Sign in as Department steward",
    ]);
  });

  /** A server where the session turns signed-in after the sign-in POST. */
  function signInRoutes(onPost: (body: unknown) => void, fail = false) {
    let signedIn = false;
    return {
      "GET /api/demo/": () => json(DEMO),
      "GET /api/accounts/session": () =>
        signedIn
          ? json({ signed_in: true, ...ident })
          : json({ signed_in: false, signup_open: true }),
      "POST /api/accounts/sign-in": async (req: Request) => {
        onPost(await req.json());
        if (fail) return problem(401, { code: "unauthenticated" });
        signedIn = true;
        return new Response(null, { status: 204 });
      },
    };
  }

  it("sign in as fills the form and signs in", async () => {
    let sent: unknown;
    const { router } = mountApp({
      routes: signInRoutes((b) => (sent = b)),
      path: "/accounts/sign-in",
    });
    await userEvent.click(
      await screen.findByRole("button", { name: "Sign in as Lead: Rangers, Gate, DPW" }),
    );
    await waitFor(() =>
      expect(sent).toEqual({ email: "lead@demo.invalid", password: "correct-horse-battery" }),
    );
    await waitFor(() => expect(router()!.state.location.pathname).toBe("/"));
    expect(await screen.findByRole("button", { name: "Sign out" })).toBeInTheDocument();
  });

  it("sign in as keeps returnTo", async () => {
    const { router } = mountApp({
      routes: signInRoutes(() => {}),
      path: "/accounts/sign-in?returnTo=%2Fhello",
    });
    await userEvent.click(await screen.findByRole("button", { name: "Sign in as Participant" }));
    await waitFor(() => expect(router()!.state.location.pathname).toBe("/hello"));
  });

  it("a request made before leaving the sign-in page is not replayed when it mounts again", async () => {
    let calls = 0;
    const { router } = mountApp({
      routes: signInRoutes(() => calls++, true),
      path: "/accounts/sign-in",
    });
    await userEvent.click(await screen.findByRole("button", { name: "Sign in as Participant" }));
    await screen.findByRole("alert");
    expect(calls).toBe(1);
    await router()!.navigate("/hello");
    await screen.findByRole("heading", { level: 1, name: /Hello/ });
    await router()!.navigate("/accounts/sign-in");
    await screen.findByRole("heading", { name: "Sign in" });
    expect(screen.getByLabelText(/E-mail address/i)).toHaveValue("");
    expect(calls).toBe(1);
  });

  it("without a shared password there is no Sign in column", async () => {
    mountApp({
      routes: { "GET /api/demo/": () => json({ ...DEMO, password: "" }) },
      path: "/accounts/sign-in",
    });
    const table = await screen.findByRole("table", { name: "Demo accounts" });
    expect(within(table).queryByRole("button")).toBeNull();
  });

  it("sign in as failure shows the form's error, moves focus to it and leaves the buttons usable", async () => {
    let calls = 0;
    mountApp({
      routes: {
        "GET /api/demo/": () => json(DEMO),
        "POST /api/accounts/sign-in": () => {
          calls++;
          return problem(401, { code: "unauthenticated" });
        },
      },
      path: "/accounts/sign-in",
    });
    const button = await screen.findByRole("button", { name: "Sign in as Participant" });
    await userEvent.click(button);
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(/isn't right/i);
    await waitFor(() => expect(alert).toHaveFocus());
    expect(button).not.toHaveAttribute("aria-disabled");
    await userEvent.click(button);
    await waitFor(() => expect(calls).toBe(2));
  });

  it("while a sign-in runs the buttons are busy and a second press sends nothing", async () => {
    let calls = 0;
    let release: () => void = () => {};
    mountApp({
      routes: {
        "GET /api/demo/": () => json(DEMO),
        "POST /api/accounts/sign-in": async () => {
          calls++;
          await new Promise<void>((r) => (release = r));
          return problem(401, { code: "unauthenticated" });
        },
      },
      path: "/accounts/sign-in",
    });
    const first = await screen.findByRole("button", { name: "Sign in as Participant" });
    const second = screen.getByRole("button", { name: "Sign in as Department steward" });
    await userEvent.click(first);
    await waitFor(() => expect(first).toHaveAttribute("aria-disabled", "true"));
    expect(second).toHaveAttribute("aria-disabled", "true");
    await userEvent.click(second);
    await userEvent.click(first);
    expect(calls).toBe(1);
    expect(screen.getByLabelText(/E-mail address/i)).toHaveValue("participant@demo.invalid");
    release();
    await screen.findByRole("alert");
    expect(first).not.toHaveAttribute("aria-disabled");
  });

  it("links to the captured mail when signed in", async () => {
    mountApp({ session: ident, routes: { "GET /api/demo/": () => json(DEMO) } });
    const link = await screen.findByRole("link", { name: "Captured mail" });
    expect(link).toHaveAttribute("href", "/demo/mail");
  });

  it("shows nothing when demo mode is off (enabled:false, or an older server's 404)", async () => {
    mountApp({ routes: { "GET /api/demo/": () => json({ enabled: false }) } });
    await screen.findByRole("heading", { level: 1 });
    expect(screen.queryByRole("note")).not.toBeInTheDocument();
  });

  it("an older server without the route is not a demo and is not reported", async () => {
    const sent: Batch[] = [];
    installReporter({
      appVersion: "dev",
      send: (b) => (sent.push(b), Promise.resolve({ kind: "accepted" })),
    });
    mountApp();
    await screen.findByRole("heading", { level: 1 });
    expect(screen.queryByRole("note")).not.toBeInTheDocument();
    expect(sent).toHaveLength(0);
  });

  it("F40: a demo read failure hides the banner, reports, and offers a retry that works", async () => {
    vi.spyOn(console, "debug").mockImplementation(() => {});
    const reporter = installReporter({
      appVersion: "dev",
      send: () => Promise.resolve({ kind: "accepted" }),
    });
    const spy = vi.spyOn(reporter, "screenCrash");
    let fail = true;
    mountApp({
      routes: {
        "GET /api/demo/": () =>
          fail ? problem(500, { code: "internal", ref: "AAAA-0000" }) : json(DEMO),
      },
    });
    expect(
      await screen.findByText("Couldn't check whether this is a public demo."),
    ).toBeInTheDocument();
    await waitFor(() => expect(spy).toHaveBeenCalled());
    expect(screen.queryByRole("note")).not.toBeInTheDocument();
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("note")).toBeInTheDocument();
    expect(
      screen.queryByText("Couldn't check whether this is a public demo."),
    ).not.toBeInTheDocument();
  });

  it("on a phone the accounts open from a 44 px summary; the sign-in form stays near the top", async () => {
    vi.stubGlobal("matchMedia", (q: string) => ({
      matches: false,
      media: q,
      addEventListener() {},
      removeEventListener() {},
    }));
    const { container } = mountApp({
      routes: { "GET /api/demo/": () => json(DEMO) },
      path: "/accounts/sign-in",
    });
    const summary = await screen.findByText("Try a demo account");
    expect(summary.tagName).toBe("SUMMARY");
    expect(summary.closest("details")).not.toHaveAttribute("open");
    expect(summary.className).toContain("min-h-11");
    expect(screen.queryByRole("heading", { name: "Try a demo account" })).not.toBeInTheDocument();
    await userEvent.click(summary);
    expect(summary.closest("details")).toHaveAttribute("open");
    expect(
      screen.getByText("Password for every demo account: correct-horse-battery"),
    ).toBeInTheDocument();
    expect(screen.getByRole("table", { name: "Demo accounts" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Sign in as Participant" })).toBeInTheDocument();
    await expectNoAxeViolations(container);
    vi.unstubAllGlobals();
  });

  it("F42: sign-up shows the made-up address note", async () => {
    mountApp({
      routes: {
        "GET /api/demo/": () => json(DEMO),
        "GET /api/accounts/session": () => json({ signed_in: false, signup_open: true }),
      },
      path: "/accounts/sign-up",
    });
    const email = await screen.findByLabelText(/E-mail address/i);
    expect(email).toHaveAccessibleDescription(/Use a made-up address, not your own/);
  });

  it("sign-up has no demo note when demo mode is off", async () => {
    mountApp({
      routes: {
        "GET /api/accounts/session": () => json({ signed_in: false, signup_open: true }),
      },
      path: "/accounts/sign-up",
    });
    const email = await screen.findByLabelText(/E-mail address/i);
    expect(email).not.toHaveAccessibleDescription(/made-up address/);
  });
});

describe("a malformed demo answer", () => {
  it("drops bad account rows instead of crashing the frame", async () => {
    mountApp({
      routes: {
        "GET /api/demo/": () =>
          json({
            ...DEMO,
            accounts: [DEMO.accounts[0], { key: "x", email: "x@demo.invalid", roles: null }, null],
          }),
      },
      path: "/accounts/sign-in",
    });
    const table = await screen.findByRole("table", { name: "Demo accounts" });
    expect(within(table).getAllByRole("row")).toHaveLength(2);
  });
});

describe("protected accounts", () => {
  it("F44: a protected account shows the shared-account message", async () => {
    mountApp({
      session: ident,
      path: "/accounts/password",
      routes: {
        "POST /api/accounts/password": () =>
          problem(403, {
            code: "permission",
            fields: [{ field: "account", code: "protected", reason: "protected" }],
          }),
      },
    });
    await userEvent.type(await screen.findByLabelText(/Current password/i), "x");
    await userEvent.type(screen.getByLabelText(/New password/i), "another-long-password");
    await userEvent.click(screen.getByRole("button", { name: /change password|save|update/i }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(
      "This is a shared demo account. Its password and e-mail address can't be changed.",
    );
    expect(alert).not.toHaveTextContent("don't have permission");
  });
});
