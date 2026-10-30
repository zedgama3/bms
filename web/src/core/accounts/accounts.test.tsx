import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { expectNoAxeViolations } from "@/test/axe";
import { mountApp } from "@/test/app";
import { json, noContent, problem } from "@/test/fakeFetch";

const ME = "0b6f1c3e-0000-4000-8000-000000000002";
const OTHER = "0b6f1c3e-0000-4000-8000-000000000003";
const SUPER = "0b6f1c3e-0000-4000-8000-000000000004";

const ident = {
  person_id: "7d2c2f0e-0000-4000-8000-000000000001",
  account_id: ME,
  playa_name: "Dusty",
  superuser: false,
  password_change_required: false,
  expires_at: "2026-11-17T10:00:00Z",
  signup_open: true,
  permissions: [],
};

const account = (over: Record<string, unknown> = {}) => ({
  id: OTHER,
  person_id: "7d2c2f0e-0000-4000-8000-000000000009",
  email: "pat@example.test",
  superuser: false,
  password_change_required: false,
  password_changed_at: "2026-10-01T10:00:00Z",
  created_at: "2026-10-01T10:00:00Z",
  updated_at: "2026-10-01T10:00:00Z",
  ...over,
});

type Routes = Record<string, (req: Request) => Response | Promise<Response>>;

/** A server where identity exists: signed out until sign-in or sign-up succeeds. */
function identityRoutes(over: Routes = {}, signupOpen = true): Routes {
  let signedIn = false;
  return {
    "GET /api/accounts/session": () =>
      signedIn
        ? json({ signed_in: true, ...ident })
        : json({ signed_in: false, signup_open: signupOpen }),
    "POST /api/accounts/sign-in": () => {
      signedIn = true;
      return noContent();
    },
    "POST /api/accounts/sign-up": () => {
      signedIn = true;
      return new Response(null, { status: 201 });
    },
    ...over,
  };
}

const bodyOf = async (fetchMock: { mock: { calls: [Request][] } }, path: string) => {
  const hit = fetchMock.mock.calls.find(([r]) => new URL(r.url).pathname === path);
  return hit ? ((await hit[0].clone().json()) as unknown) : undefined;
};

async function fill(label: RegExp, value: string) {
  await userEvent.type(await screen.findByLabelText(label), value);
}

describe("/accounts/sign-in", () => {
  it("has labelled inputs with the right type, inputmode and autocomplete, and is accessible", async () => {
    const { container } = mountApp({ path: "/accounts/sign-in", routes: identityRoutes() });
    const email = await screen.findByLabelText(/E-mail address/);
    const password = screen.getByLabelText(/^Password/);
    expect(email).toHaveAttribute("type", "email");
    expect(email).toHaveAttribute("inputmode", "email");
    expect(email).toHaveAttribute("autocomplete", "username");
    expect(email).toHaveAttribute("name", "email");
    expect(password).toHaveAttribute("type", "password");
    expect(password).toHaveAttribute("autocomplete", "current-password");
    expect(password).toHaveAttribute("name", "password");
    expect(screen.getByRole("button", { name: "Sign in" })).toHaveAttribute("type", "submit");
    await expectNoAxeViolations(container);
  });

  it("shows and hides the password by a button", async () => {
    mountApp({ path: "/accounts/sign-in", routes: identityRoutes() });
    const password = await screen.findByLabelText(/^Password/);
    await userEvent.click(screen.getByRole("button", { name: "Show password" }));
    expect(password).toHaveAttribute("type", "text");
    await userEvent.click(screen.getByRole("button", { name: "Hide password" }));
    expect(password).toHaveAttribute("type", "password");
  });

  it("offers sign-up only when it is open", async () => {
    mountApp({ path: "/accounts/sign-in", routes: identityRoutes() });
    expect(await screen.findByRole("link", { name: "Create an account" })).toHaveAttribute(
      "href",
      "/accounts/sign-up",
    );
  });

  it("does not offer sign-up when it is closed", async () => {
    mountApp({ path: "/accounts/sign-in", routes: identityRoutes({}, false) });
    await screen.findByLabelText(/E-mail address/);
    expect(screen.queryByRole("link", { name: "Create an account" })).not.toBeInTheDocument();
  });

  it("signs in, refreshes the session and follows returnTo", async () => {
    const { router, fetchMock } = mountApp({
      path: "/accounts/sign-in?returnTo=%2Fhello",
      routes: identityRoutes(),
    });
    await fill(/E-mail address/, "pat@example.test");
    await userEvent.type(screen.getByLabelText(/^Password/), "correct horse");
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    await waitFor(() => expect(router()!.state.location.pathname).toBe("/hello"));
    expect(await bodyOf(fetchMock, "/api/accounts/sign-in")).toEqual({
      email: "pat@example.test",
      password: "correct horse",
    });
  });

  it("F34: an open redirect is refused; the person lands on /", async () => {
    const { router } = mountApp({
      path: "/accounts/sign-in?returnTo=%2F%2Fevil.example",
      routes: identityRoutes(),
    });
    await fill(/E-mail address/, "a@b.test");
    await userEvent.type(screen.getByLabelText(/^Password/), "x");
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    await waitFor(() => expect(router()!.state.location.pathname).toBe("/"));
  });

  it("401 says the e-mail address or password isn't right, never which, and does not redirect", async () => {
    const { router } = mountApp({
      path: "/accounts/sign-in",
      routes: identityRoutes({
        "POST /api/accounts/sign-in": () => problem(401, { code: "unauthenticated" }),
      }),
    });
    await fill(/E-mail address/, "a@b.test");
    await userEvent.type(screen.getByLabelText(/^Password/), "x");
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "That e-mail address or password isn't right.",
    );
    expect(router()!.state.location.pathname).toBe("/accounts/sign-in");
    expect(screen.getByRole("button", { name: "Sign in" })).not.toHaveAttribute("aria-disabled");
  });

  it("429 shows the rate-limit sentence with the seconds", async () => {
    mountApp({
      path: "/accounts/sign-in",
      routes: identityRoutes({
        "POST /api/accounts/sign-in": () =>
          problem(429, { code: "rate_limited" }, { "Retry-After": "20" }),
      }),
    });
    await fill(/E-mail address/, "a@b.test");
    await userEvent.type(screen.getByLabelText(/^Password/), "x");
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Wait 20 seconds and try again.");
  });

  it("a server failure is shown with its reference", async () => {
    mountApp({
      path: "/accounts/sign-in",
      routes: identityRoutes({
        "POST /api/accounts/sign-in": () => problem(500, { code: "internal", ref: "AB12-CD34" }),
      }),
    });
    await fill(/E-mail address/, "a@b.test");
    await userEvent.type(screen.getByLabelText(/^Password/), "x");
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("AB12-CD34");
  });

  it("works on a read-only copy: sign-in is not disabled", async () => {
    mountApp({ path: "/accounts/sign-in", routes: identityRoutes(), readOnly: () => true });
    await screen.findByLabelText(/E-mail address/);
    expect(screen.getByRole("button", { name: "Sign in" })).toBeEnabled();
  });
});

describe("/accounts/sign-up", () => {
  it("has the right input attributes, states the rule under the password, and is accessible", async () => {
    const { container } = mountApp({ path: "/accounts/sign-up", routes: identityRoutes() });
    const email = await screen.findByLabelText(/E-mail address/);
    const password = screen.getByLabelText(/^Password/);
    expect(email).toHaveAttribute("autocomplete", "username");
    expect(email).toHaveAttribute("type", "email");
    expect(password).toHaveAttribute("autocomplete", "new-password");
    expect(password).toHaveAccessibleDescription(
      "At least 12 characters, and not a common password.",
    );
    await expectNoAxeViolations(container);
  });

  it("creates the account and follows returnTo", async () => {
    const { router, fetchMock } = mountApp({
      path: "/accounts/sign-up?returnTo=%2Fhello",
      routes: identityRoutes(),
    });
    await fill(/E-mail address/, "new@example.test");
    await userEvent.type(screen.getByLabelText(/^Password/), "a long enough password");
    await userEvent.click(screen.getByRole("button", { name: "Create account" }));
    await waitFor(() => expect(router()!.state.location.pathname).toBe("/hello"));
    expect(await bodyOf(fetchMock, "/api/accounts/sign-up")).toEqual({
      email: "new@example.test",
      password: "a long enough password",
    });
  });

  it("puts each field error on its input, by code and parameters", async () => {
    mountApp({
      path: "/accounts/sign-up",
      routes: identityRoutes({
        "POST /api/accounts/sign-up": () =>
          problem(400, {
            code: "invalid",
            fields: [
              { field: "email", code: "malformed", reason: "x" },
              { field: "password", code: "too_short", params: { min: "14" }, reason: "x" },
            ],
          }),
      }),
    });
    await fill(/E-mail address/, "nope");
    await userEvent.type(screen.getByLabelText(/^Password/), "short");
    await userEvent.click(screen.getByRole("button", { name: "Create account" }));
    await screen.findByRole("alert");
    expect(screen.getByLabelText(/E-mail address/)).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByLabelText(/^Password/)).toHaveAttribute("aria-invalid", "true");
    expect(screen.getAllByText(/Must be at least 14 characters\./).length).toBeGreaterThan(0);
  });

  it.each([
    ["too_long", { max: "256" }, "Must be at most 256 characters."],
    ["common", undefined, "That password is too common. Choose another."],
  ])("password code %s", async (code, params, sentence) => {
    mountApp({
      path: "/accounts/sign-up",
      routes: identityRoutes({
        "POST /api/accounts/sign-up": () =>
          problem(400, {
            code: "invalid",
            fields: [{ field: "password", code, ...(params ? { params } : {}), reason: "x" }],
          }),
      }),
    });
    await fill(/E-mail address/, "a@b.test");
    await userEvent.type(screen.getByLabelText(/^Password/), "x");
    await userEvent.click(screen.getByRole("button", { name: "Create account" }));
    await screen.findByRole("alert");
    expect(screen.getAllByText(sentence).length).toBeGreaterThan(0);
  });

  it.each([
    ["the real body: no fields, with a reference", { ref: "AB12-CD34" }],
    ["a body that names the field", { fields: [{ field: "email", code: "taken", reason: "x" }] }],
  ])("409 taken (%s) lands on the e-mail field, with no reference line", async (_n, extra) => {
    mountApp({
      path: "/accounts/sign-up",
      routes: identityRoutes({
        "POST /api/accounts/sign-up": () => problem(409, { code: "conflict", ...extra }),
      }),
    });
    await fill(/E-mail address/, "a@b.test");
    await userEvent.type(screen.getByLabelText(/^Password/), "x");
    await userEvent.click(screen.getByRole("button", { name: "Create account" }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("That e-mail address already has an account.");
    expect(alert).not.toHaveTextContent("AB12-CD34");
    expect(screen.getByLabelText(/E-mail address/)).toHaveAttribute("aria-invalid", "true");
    expect(screen.getAllByText("This is already in use.").length).toBeGreaterThan(0);
  });

  it("when the session check failed it does not claim sign-up is by invitation", async () => {
    mountApp({ path: "/accounts/sign-up", session: "fail" });
    expect(await screen.findByRole("alert")).toHaveTextContent("isn't available right now");
    expect(screen.queryByText("This regional creates accounts by invitation.")).toBeNull();
  });

  it("501 (invitation mode) says accounts are by invitation", async () => {
    mountApp({
      path: "/accounts/sign-up",
      routes: identityRoutes({
        "POST /api/accounts/sign-up": () => problem(501, { code: "unsupported" }),
      }),
    });
    await fill(/E-mail address/, "a@b.test");
    await userEvent.type(screen.getByLabelText(/^Password/), "x");
    await userEvent.click(screen.getByRole("button", { name: "Create account" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "This regional creates accounts by invitation.",
    );
  });

  it("when sign-up is closed there is no form, only the explanation and a way to sign in", async () => {
    const { container } = mountApp({
      path: "/accounts/sign-up",
      routes: identityRoutes({}, false),
    });
    expect(
      await screen.findByText("This regional creates accounts by invitation."),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText(/E-mail address/)).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Already have an account? Sign in" })).toBeVisible();
    await expectNoAxeViolations(container);
  });

  it("429 shows the rate-limit sentence", async () => {
    mountApp({
      path: "/accounts/sign-up",
      routes: identityRoutes({
        "POST /api/accounts/sign-up": () =>
          problem(429, { code: "rate_limited" }, { "Retry-After": "60" }),
      }),
    });
    await fill(/E-mail address/, "a@b.test");
    await userEvent.type(screen.getByLabelText(/^Password/), "x");
    await userEvent.click(screen.getByRole("button", { name: "Create account" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Wait 60 seconds");
  });

  it("read-only copy: the button is disabled and says why", async () => {
    const { container } = mountApp({
      path: "/accounts/sign-up",
      routes: identityRoutes(),
      readOnly: () => true,
    });
    const button = await screen.findByRole("button", { name: "Create account" });
    await waitFor(() => expect(button).toBeDisabled());
    expect(button).toHaveAccessibleDescription(
      "This copy is read-only, so this can't be saved right now.",
    );
    await expectNoAxeViolations(container);
  });

  it("503 read_only is explained in the shell's wording", async () => {
    mountApp({
      path: "/accounts/sign-up",
      routes: identityRoutes({
        "POST /api/accounts/sign-up": () => problem(503, { code: "read_only" }),
      }),
    });
    await fill(/E-mail address/, "a@b.test");
    await userEvent.type(screen.getByLabelText(/^Password/), "x");
    await userEvent.click(screen.getByRole("button", { name: "Create account" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "This copy is read-only right now, so that can't be saved.",
    );
  });
});

describe("/accounts/password", () => {
  const signedIn = { path: "/accounts/password", session: ident };

  it("labels, autocomplete, and the second section, accessibly", async () => {
    const { container } = mountApp(signedIn);
    const current = await screen.findByLabelText(/^Current password/);
    const next = screen.getByLabelText(/^New password/);
    expect(current).toHaveAttribute("autocomplete", "current-password");
    expect(current).toHaveAttribute("name", "current_password");
    expect(next).toHaveAttribute("autocomplete", "new-password");
    expect(next).toHaveAttribute("name", "new_password");
    expect(screen.getByRole("heading", { level: 1, name: "Account" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 2, name: "Change password" })).toBeInTheDocument();
    // The h1 is outside the labelled section, which is named by the h2.
    expect(screen.getByRole("region", { name: "Change password" })).not.toContainElement(
      screen.getByRole("heading", { level: 1, name: "Account" }),
    );
    expect(
      screen.getByRole("heading", { level: 2, name: "Sign out everywhere else" }),
    ).toBeInTheDocument();
    expect(screen.queryByText("You must choose a new password before you continue.")).toBeNull();
    await expectNoAxeViolations(container);
  });

  it("a protected demo account sees the shared-account notice and a disabled form", async () => {
    const { container } = mountApp({ ...signedIn, session: { ...ident, protected: true } });
    expect(
      await screen.findByText(
        "This is a shared demo account. Its password and e-mail address can't be changed.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByLabelText(/^Current password/)).toBeDisabled();
    expect(screen.getByLabelText(/^New password/)).toBeDisabled();
    expect(screen.getByRole("button", { name: "Change password" })).toBeDisabled();
    // Ending the other sessions stays allowed (demo.md).
    expect(screen.getByRole("button", { name: "Sign out everywhere else" })).toBeEnabled();
    await expectNoAxeViolations(container);
  });

  it("without protected there is no notice and the form works", async () => {
    mountApp(signedIn);
    expect(await screen.findByLabelText(/^Current password/)).toBeEnabled();
    expect(screen.queryByText(/shared demo account/)).toBeNull();
    expect(screen.getByRole("button", { name: "Change password" })).toBeEnabled();
  });

  it("the show buttons have their own names", async () => {
    mountApp(signedIn);
    expect(await screen.findByRole("button", { name: "Show current password" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Show new password" })).toBeVisible();
  });

  it("changes the password, toasts, and clears the form", async () => {
    const { fetchMock } = mountApp({
      ...signedIn,
      routes: { "POST /api/accounts/password": () => noContent() },
    });
    await fill(/^Current password/, "old password 1");
    await userEvent.type(screen.getByLabelText(/^New password/), "new password 22");
    await userEvent.click(screen.getByRole("button", { name: "Change password" }));
    expect(await screen.findByText("Password changed")).toBeInTheDocument();
    expect(await bodyOf(fetchMock, "/api/accounts/password")).toEqual({
      current_password: "old password 1",
      new_password: "new password 22",
    });
    expect(screen.getByLabelText(/^Current password/)).toHaveValue("");
  });

  it("incorrect lands on the current password field", async () => {
    mountApp({
      ...signedIn,
      routes: {
        "POST /api/accounts/password": () =>
          problem(400, {
            code: "invalid",
            fields: [{ field: "current_password", code: "incorrect", reason: "x" }],
          }),
      },
    });
    await fill(/^Current password/, "wrong");
    await userEvent.type(screen.getByLabelText(/^New password/), "new password 22");
    await userEvent.click(screen.getByRole("button", { name: "Change password" }));
    await screen.findByRole("alert");
    expect(screen.getByLabelText(/^Current password/)).toHaveAttribute("aria-invalid", "true");
    expect(screen.getAllByText("That isn't right.").length).toBeGreaterThan(0);
  });

  it("a restricted session sees the banner, no sign-out-everywhere section, and moves on to returnTo", async () => {
    const restricted = { ...ident, password_change_required: true };
    let changed = false;
    const { router, container } = mountApp({
      path: "/accounts/password?returnTo=%2Fhello",
      routes: {
        "GET /api/accounts/session": () =>
          json({ signed_in: true, ...restricted, password_change_required: !changed }),
        "POST /api/accounts/password": () => {
          changed = true;
          return noContent();
        },
      },
    });
    expect(
      await screen.findByText("You must choose a new password before you continue."),
    ).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Sign out everywhere else" })).toBeNull();
    await expectNoAxeViolations(container);
    await fill(/^Current password/, "temporary pw 1");
    await userEvent.type(screen.getByLabelText(/^New password/), "new password 22");
    await userEvent.click(screen.getByRole("button", { name: "Change password" }));
    await waitFor(() => expect(router()!.state.location.pathname).toBe("/hello"));
  });

  it("signs out everywhere else after a confirmation", async () => {
    const calls = vi.fn();
    mountApp({
      ...signedIn,
      routes: {
        "POST /api/accounts/sessions/end-others": () => {
          calls();
          return noContent();
        },
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: "Sign out everywhere else" }));
    const dialog = await screen.findByRole("dialog", { name: "Sign out everywhere else?" });
    expect(calls).not.toHaveBeenCalled();
    await userEvent.click(within(dialog).getByRole("button", { name: "Sign out other sessions" }));
    expect(await screen.findByText("Signed out everywhere else")).toBeInTheDocument();
    expect(calls).toHaveBeenCalledTimes(1);
  });

  it("cancelling the confirmation calls nothing", async () => {
    const calls = vi.fn();
    mountApp({
      ...signedIn,
      routes: {
        "POST /api/accounts/sessions/end-others": () => {
          calls();
          return noContent();
        },
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: "Sign out everywhere else" }));
    const dialog = await screen.findByRole("dialog", { name: "Sign out everywhere else?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(calls).not.toHaveBeenCalled();
  });

  it("a failure in the confirmation is shown inside it", async () => {
    mountApp({
      ...signedIn,
      routes: {
        "POST /api/accounts/sessions/end-others": () => problem(503, { code: "read_only" }),
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: "Sign out everywhere else" }));
    const dialog = await screen.findByRole("dialog", { name: "Sign out everywhere else?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Sign out other sessions" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("read-only");
  });

  it("read-only copy: both writes are disabled with the reason", async () => {
    mountApp({ ...signedIn, readOnly: () => true });
    const change = await screen.findByRole("button", { name: "Change password" });
    await waitFor(() => expect(change).toBeDisabled());
    expect(screen.getByRole("button", { name: "Sign out everywhere else" })).toBeDisabled();
    expect(change).toHaveAccessibleDescription(/read-only/);
  });

  it("signed out it goes to sign-in", async () => {
    const { router } = mountApp({
      path: "/accounts/password",
      routes: identityRoutes(),
    });
    await waitFor(() => expect(router()!.state.location.pathname).toBe("/accounts/sign-in"));
    expect(router()!.state.location.search).toBe("?returnTo=%2Faccounts%2Fpassword");
  });
});

const admin = { ...ident, permissions: ["accounts.read", "accounts.manage"] };

describe("/accounts/admin", () => {
  it("lists accounts with their state, each linking to its detail, accessibly", async () => {
    const { container } = mountApp({
      path: "/accounts/admin",
      session: admin,
      routes: {
        "GET /api/accounts/": () =>
          json({
            accounts: [
              account({ last_login_at: "2026-10-10T10:00:00Z" }),
              account({
                id: SUPER,
                email: "root@example.test",
                superuser: true,
                disabled_at: "2026-10-11T10:00:00Z",
              }),
            ],
          }),
      },
    });
    const link = await screen.findByRole("link", { name: "pat@example.test" });
    expect(link).toHaveAttribute("href", `/accounts/admin/${OTHER}`);
    expect(screen.getByText("Disabled and Superuser")).toBeInTheDocument();
    expect(screen.getByText("Never")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
    await expectNoAxeViolations(container);
  });

  it("is empty with a sentence", async () => {
    mountApp({
      path: "/accounts/admin",
      session: admin,
      routes: { "GET /api/accounts/": () => json({ accounts: [] }) },
    });
    expect(await screen.findByText("No accounts yet.")).toBeInTheDocument();
  });

  it("403 shows the forbidden sentence", async () => {
    mountApp({
      path: "/accounts/admin",
      session: ident,
      routes: { "GET /api/accounts/": () => problem(403, { code: "permission" }) },
    });
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "You don't have permission to see accounts.",
    );
  });

  it("403: no retry button and no search form", async () => {
    mountApp({
      path: "/accounts/admin",
      session: ident,
      routes: { "GET /api/accounts/": () => problem(403, { code: "permission" }) },
    });
    await screen.findByRole("alert");
    expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
    expect(screen.queryByLabelText(/E-mail address/)).toBeNull();
  });

  it("a server 400 on the search lands on the e-mail input", async () => {
    mountApp({
      path: "/accounts/admin",
      session: admin,
      routes: {
        "GET /api/accounts/": (req) =>
          new URL(req.url).searchParams.get("email")
            ? problem(400, {
                code: "invalid",
                fields: [{ field: "email", code: "malformed", reason: "x" }],
              })
            : json({ accounts: [account()] }),
      },
    });
    await screen.findByRole("link", { name: "pat@example.test" });
    await userEvent.type(screen.getByLabelText(/E-mail address/), "nope");
    await userEvent.click(screen.getByRole("button", { name: "Search" }));
    await screen.findByRole("alert");
    expect(screen.getByLabelText(/E-mail address/)).toHaveAttribute("aria-invalid", "true");
  });

  it("an error has a retry", async () => {
    let fail = true;
    mountApp({
      path: "/accounts/admin",
      session: admin,
      routes: {
        "GET /api/accounts/": () =>
          fail
            ? problem(500, { code: "internal", ref: "AB12-CD34" })
            : json({ accounts: [account()] }),
      },
    });
    expect(await screen.findByRole("alert")).toHaveTextContent("AB12-CD34");
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("link", { name: "pat@example.test" })).toBeInTheDocument();
  });

  it("pages with Load more by the cursor", async () => {
    const afters: (string | null)[] = [];
    mountApp({
      path: "/accounts/admin",
      session: admin,
      routes: {
        "GET /api/accounts/": (req) => {
          const after = new URL(req.url).searchParams.get("after");
          afters.push(after);
          return after
            ? json({ accounts: [account({ id: SUPER, email: "second@example.test" })] })
            : json({ accounts: [account()], next: "cursor-1" });
        },
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: "Load more" }));
    expect(await screen.findByRole("link", { name: "second@example.test" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "pat@example.test" })).toBeInTheDocument();
    expect(afters).toEqual([null, "cursor-1"]);
    expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
  });

  it("searches by exact address without putting it in the page URL, and says when nothing matches", async () => {
    const { router } = mountApp({
      path: "/accounts/admin",
      session: admin,
      routes: {
        "GET /api/accounts/": (req) =>
          new URL(req.url).searchParams.get("email")
            ? json({ accounts: [] })
            : json({ accounts: [account()] }),
      },
    });
    await screen.findByRole("link", { name: "pat@example.test" });
    await userEvent.type(screen.getByLabelText(/E-mail address/), "nobody@example.test");
    await userEvent.click(screen.getByRole("button", { name: "Search" }));
    expect(await screen.findByText("No account has that address.")).toBeInTheDocument();
    expect(router()!.state.location.search).toBe("");
    await userEvent.click(screen.getByRole("button", { name: "Show newest accounts" }));
    expect(await screen.findByRole("link", { name: "pat@example.test" })).toBeInTheDocument();
  });
});

describe("/accounts/admin/:account", () => {
  const accountId = (a: Record<string, unknown>): string =>
    typeof a["id"] === "string" ? a["id"] : OTHER;
  const detail = (a: Record<string, unknown>, extra: Routes = {}) => ({
    path: `/accounts/admin/${accountId(a)}`,
    session: admin,
    routes: { [`GET /api/accounts/${accountId(a)}`]: () => json(a), ...extra },
  });

  it("shows the account and accessible controls", async () => {
    const { container } = mountApp(detail(account()));
    expect(await screen.findByText("pat@example.test")).toBeInTheDocument();
    expect(screen.getByText("Active")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Disable account" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Set a temporary password" })).toBeEnabled();
    await expectNoAxeViolations(container);
  });

  it("disables after a confirmation, then toasts", async () => {
    const calls = vi.fn();
    mountApp(
      detail(account(), {
        [`POST /api/accounts/${OTHER}/disable`]: () => {
          calls();
          return noContent();
        },
      }),
    );
    await userEvent.click(await screen.findByRole("button", { name: "Disable account" }));
    const dialog = await screen.findByRole("dialog", { name: "Disable this account?" });
    expect(calls).not.toHaveBeenCalled();
    await userEvent.click(within(dialog).getByRole("button", { name: "Disable account" }));
    expect(await screen.findByText("Account disabled")).toBeInTheDocument();
    expect(calls).toHaveBeenCalledTimes(1);
  });

  it("F44: disabling a protected account shows the shared-account sentence, without a reference", async () => {
    mountApp(
      detail(account(), {
        [`POST /api/accounts/${OTHER}/disable`]: () =>
          problem(403, {
            code: "permission",
            ref: "AB12-CD34",
            fields: [{ field: "account", code: "protected", reason: "x" }],
          }),
      }),
    );
    await userEvent.click(await screen.findByRole("button", { name: "Disable account" }));
    const dialog = await screen.findByRole("dialog", { name: "Disable this account?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Disable account" }));
    const alert = await within(dialog).findByRole("alert");
    expect(alert).toHaveTextContent(
      "This is a shared demo account. Its password and e-mail address can't be changed.",
    );
    expect(alert).not.toHaveTextContent("AB12-CD34");
  });

  it("a disabled account offers Enable", async () => {
    const calls = vi.fn();
    mountApp(
      detail(account({ disabled_at: "2026-10-11T10:00:00Z" }), {
        [`POST /api/accounts/${OTHER}/enable`]: () => {
          calls();
          return noContent();
        },
      }),
    );
    await userEvent.click(await screen.findByRole("button", { name: "Enable account" }));
    const dialog = await screen.findByRole("dialog", { name: "Enable this account?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Enable account" }));
    expect(await screen.findByText("Account enabled")).toBeInTheDocument();
    expect(calls).toHaveBeenCalledTimes(1);
  });

  it("shows a temporary password once, in a code element, with a copy button", async () => {
    const writeText = vi.fn(() => Promise.resolve());
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    const { client } = mountApp(
      detail(account(), {
        [`POST /api/accounts/${OTHER}/temporary-password`]: () =>
          json({ password: "abcd2345efgh6789jkmn2345" }),
      }),
    );
    await userEvent.click(await screen.findByRole("button", { name: "Set a temporary password" }));
    const confirm = await screen.findByRole("dialog", { name: "Set a temporary password?" });
    await userEvent.click(within(confirm).getByRole("button", { name: "Set temporary password" }));
    const shown = await screen.findByRole("dialog", { name: "Temporary password" });
    const code = await within(shown).findByText("abcd2345efgh6789jkmn2345");
    expect(code.tagName).toBe("CODE");
    expect(
      within(shown).getByText("The person must change it at their next sign-in."),
    ).toBeInTheDocument();
    await userEvent.click(within(shown).getByRole("button", { name: "Copy password" }));
    expect(writeText).toHaveBeenCalledWith("abcd2345efgh6789jkmn2345");
    // not kept by the query cache, and gone once the dialog is closed
    expect(
      JSON.stringify(
        client
          .getQueryCache()
          .getAll()
          .map((q) => q.state.data),
      ),
    ).not.toContain("abcd2345");
    await userEvent.click(within(shown).getByRole("button", { name: "Close" }));
    await waitFor(() => expect(screen.queryByText("abcd2345efgh6789jkmn2345")).toBeNull());
  });

  it("disables the actions for one's own account and says why", async () => {
    mountApp(detail(account({ id: ME, email: "me@example.test" })));
    const button = await screen.findByRole("button", { name: "Disable account" });
    expect(button).toBeDisabled();
    expect(button).toHaveAccessibleDescription(
      "This is your own account, so you can't change it here.",
    );
    expect(screen.getByRole("button", { name: "Set a temporary password" })).toBeDisabled();
  });

  it("disables the actions for a superuser account and says why", async () => {
    mountApp(detail(account({ id: SUPER, superuser: true })));
    const button = await screen.findByRole("button", { name: "Disable account" });
    expect(button).toBeDisabled();
    expect(button).toHaveAccessibleDescription(
      "A superuser account can only be changed from the server.",
    );
  });

  it("read-only copy: actions disabled with the banner's wording", async () => {
    mountApp({ ...detail(account()), readOnly: () => true });
    const button = await screen.findByRole("button", { name: "Disable account" });
    await waitFor(() => expect(button).toBeDisabled());
    expect(button).toHaveAccessibleDescription(
      "This copy is read-only, so this can't be saved right now.",
    );
  });

  it("503 read_only from the server is shown in the confirmation", async () => {
    mountApp(
      detail(account(), {
        [`POST /api/accounts/${OTHER}/disable`]: () => problem(503, { code: "read_only" }),
      }),
    );
    await userEvent.click(await screen.findByRole("button", { name: "Disable account" }));
    const dialog = await screen.findByRole("dialog", { name: "Disable this account?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Disable account" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("read-only");
  });

  it("a malformed ID (400 invalid) shows the not-found sentence", async () => {
    mountApp({
      path: "/accounts/admin/not-an-id",
      session: admin,
      routes: { "GET /api/accounts/not-an-id": () => problem(400, { code: "invalid" }) },
    });
    expect(await screen.findByRole("alert")).toHaveTextContent("We couldn't find that.");
  });

  it("the temporary password stays up even when the refetch after it fails", async () => {
    let fail = false;
    mountApp({
      path: `/accounts/admin/${OTHER}`,
      session: admin,
      routes: {
        [`GET /api/accounts/${OTHER}`]: () =>
          fail ? problem(500, { code: "internal" }) : json(account()),
        [`POST /api/accounts/${OTHER}/temporary-password`]: () => {
          fail = true;
          return json({ password: "abcd2345efgh6789jkmn2345" });
        },
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: "Set a temporary password" }));
    const confirm = await screen.findByRole("dialog", { name: "Set a temporary password?" });
    await userEvent.click(within(confirm).getByRole("button", { name: "Set temporary password" }));
    expect(await screen.findByText("abcd2345efgh6789jkmn2345")).toBeVisible();
  });

  it("a failed refetch keeps the details and shows an error with a retry", async () => {
    let fail = false;
    const { client } = mountApp({
      path: `/accounts/admin/${OTHER}`,
      session: admin,
      routes: {
        [`GET /api/accounts/${OTHER}`]: () =>
          fail ? problem(500, { code: "internal", ref: "AB12-CD34" }) : json(account()),
      },
    });
    expect(await screen.findByText("pat@example.test")).toBeInTheDocument();
    fail = true;
    await client.refetchQueries({ queryKey: ["accounts", "account", OTHER] });
    expect(await screen.findByRole("alert")).toHaveTextContent("AB12-CD34");
    expect(screen.getByText("pat@example.test")).toBeInTheDocument();
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
  });

  it("404 is the not-found state with a way back", async () => {
    mountApp({
      path: `/accounts/admin/${OTHER}`,
      session: admin,
      routes: { [`GET /api/accounts/${OTHER}`]: () => problem(404, { code: "not_found" }) },
    });
    expect(await screen.findByRole("alert")).toHaveTextContent("We couldn't find that.");
    expect(screen.getByRole("link", { name: "Back to accounts" })).toHaveAttribute(
      "href",
      "/accounts/admin",
    );
  });

  it("403 shows the forbidden sentence", async () => {
    mountApp({
      path: `/accounts/admin/${OTHER}`,
      session: admin,
      routes: { [`GET /api/accounts/${OTHER}`]: () => problem(403, { code: "permission" }) },
    });
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "You don't have permission to see accounts.",
    );
  });
});

describe("nothing personal is stored in the browser", () => {
  it("signing in writes no local or session storage", async () => {
    mountApp({ path: "/accounts/sign-in", routes: identityRoutes() });
    await fill(/E-mail address/, "pat@example.test");
    await userEvent.type(screen.getByLabelText(/^Password/), "correct horse");
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    await waitFor(() => expect(screen.getByRole("heading", { level: 1 })).toBeInTheDocument());
    const stored = JSON.stringify({ ...localStorage }) + JSON.stringify({ ...sessionStorage });
    expect(stored).not.toContain("pat@example.test");
    expect(stored).not.toContain("correct horse");
  });
});
