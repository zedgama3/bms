import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { expectNoAxeViolations } from "@/test/axe";
import { mountApp } from "@/test/app";
import { json, noContent, problem } from "@/test/fakeFetch";

type Routes = Record<string, (req: Request) => Response | Promise<Response>>;

const signedOut: Routes = {
  "GET /api/accounts/session": () => json({ signed_in: false, signup_open: true }),
};
const accepted = () => new Response(null, { status: 202 });
const SECRET = "tok-4b1d-secret-value";

const bodies = async (fetchMock: { mock: { calls: [Request][] } }, path: string) => {
  const out: unknown[] = [];
  for (const [r] of fetchMock.mock.calls) {
    if (new URL(r.url).pathname === path) out.push(await r.clone().json());
  }
  return out;
};

describe("sign-in links to the reset request", () => {
  it("has a Forgot your password? link", async () => {
    mountApp({ path: "/accounts/sign-in", routes: signedOut });
    expect(await screen.findByRole("link", { name: "Forgot your password?" })).toHaveAttribute(
      "href",
      "/accounts/password-reset",
    );
  });
});

describe("/accounts/password-reset (request)", () => {
  it("has a labelled e-mail input with the right attributes, and is accessible", async () => {
    const { container } = mountApp({
      path: "/accounts/password-reset",
      routes: { ...signedOut },
    });
    const email = await screen.findByLabelText(/E-mail address/);
    expect(email).toHaveAttribute("type", "email");
    expect(email).toHaveAttribute("inputmode", "email");
    expect(email).toHaveAttribute("autocomplete", "username");
    expect(email).toHaveAttribute("name", "email");
    expect(screen.getByRole("button", { name: "Send the link" })).toHaveAttribute("type", "submit");
    await expectNoAxeViolations(container);
  });

  it("202 shows the same sentence, whatever the address, and sends only the address", async () => {
    const a = mountApp({
      path: "/accounts/password-reset",
      routes: { ...signedOut, "POST /api/accounts/password-reset": accepted },
    });
    await userEvent.type(await screen.findByLabelText(/E-mail address/), "known@example.test");
    await userEvent.click(screen.getByRole("button", { name: "Send the link" }));
    const first = (await screen.findByText(/If an account exists/)).closest('[role="status"]')!;
    expect(first).toHaveTextContent(
      "If an account exists for that address, an e-mail is on its way.",
    );
    expect(await bodies(a.fetchMock, "/api/accounts/password-reset")).toEqual([
      { email: "known@example.test" },
    ]);
    const text = first.textContent;
    a.unmount();

    mountApp({
      path: "/accounts/password-reset",
      routes: { ...signedOut, "POST /api/accounts/password-reset": accepted },
    });
    await userEvent.type(await screen.findByLabelText(/E-mail address/), "nobody@example.test");
    await userEvent.click(screen.getByRole("button", { name: "Send the link" }));
    expect(
      (await screen.findByText(/If an account exists/)).closest('[role="status"]')!.textContent,
    ).toBe(text);
    expect(screen.getByRole("link", { name: "Back to sign in" })).toHaveAttribute(
      "href",
      "/accounts/sign-in",
    );
  });

  it("501 says e-mail isn't set up, in the same words for every address", async () => {
    const run = async (address: string) => {
      const view = mountApp({
        path: "/accounts/password-reset",
        routes: {
          ...signedOut,
          "POST /api/accounts/password-reset": () => problem(501, { code: "unsupported" }),
        },
      });
      await userEvent.type(await screen.findByLabelText(/E-mail address/), address);
      await userEvent.click(screen.getByRole("button", { name: "Send the link" }));
      const alert = await screen.findByRole("alert");
      const text = alert.textContent;
      view.unmount();
      return text;
    };
    const known = await run("known@example.test");
    const unknown = await run("nobody@example.test");
    expect(known).toContain(
      "Password reset by e-mail isn't set up here. Ask an administrator for a temporary password.",
    );
    expect(unknown).toBe(known);
  });

  it("429 shows the rate-limit sentence with the seconds", async () => {
    mountApp({
      path: "/accounts/password-reset",
      routes: {
        ...signedOut,
        "POST /api/accounts/password-reset": () =>
          problem(429, { code: "rate_limited" }, { "Retry-After": "30" }),
      },
    });
    await userEvent.type(await screen.findByLabelText(/E-mail address/), "a@example.test");
    await userEvent.click(screen.getByRole("button", { name: "Send the link" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Too many attempts. Wait 30 seconds and try again.",
    );
  });

  it("a malformed address lands on the e-mail input", async () => {
    mountApp({
      path: "/accounts/password-reset",
      routes: {
        ...signedOut,
        "POST /api/accounts/password-reset": () =>
          problem(400, { code: "invalid", fields: [{ field: "email", code: "malformed" }] }),
      },
    });
    await userEvent.type(await screen.findByLabelText(/E-mail address/), "nope");
    await userEvent.click(screen.getByRole("button", { name: "Send the link" }));
    const input = await screen.findByLabelText(/E-mail address/);
    await waitFor(() => expect(input).toHaveAttribute("aria-invalid", "true"));
    expect(screen.getAllByText(/isn't in the expected form/).length).toBeGreaterThan(0);
  });

  it("a server failure is shown with its reference", async () => {
    mountApp({
      path: "/accounts/password-reset",
      routes: {
        ...signedOut,
        "POST /api/accounts/password-reset": () =>
          problem(500, { code: "internal", ref: "ABCD-1234" }),
      },
    });
    await userEvent.type(await screen.findByLabelText(/E-mail address/), "a@example.test");
    await userEvent.click(screen.getByRole("button", { name: "Send the link" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("ABCD-1234");
  });

  it("read-only copy: the button is disabled and says why", async () => {
    mountApp({
      path: "/accounts/password-reset",
      routes: signedOut,
      readOnly: () => true,
    });
    const button = await screen.findByRole("button", { name: "Send the link" });
    await waitFor(() => expect(button).toBeDisabled());
    expect(screen.getAllByText(/read-only/).length).toBeGreaterThan(0);
  });

  it("503 read_only is explained in the shell's wording", async () => {
    mountApp({
      path: "/accounts/password-reset",
      routes: {
        ...signedOut,
        "POST /api/accounts/password-reset": () => problem(503, { code: "read_only" }),
      },
    });
    await userEvent.type(await screen.findByLabelText(/E-mail address/), "a@example.test");
    await userEvent.click(screen.getByRole("button", { name: "Send the link" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/read-only/i);
  });
});

describe("/accounts/password-reset?token= (complete)", () => {
  const open = (routes: Routes = {}) =>
    mountApp({
      path: `/accounts/password-reset?token=${SECRET}`,
      routes: { ...signedOut, ...routes },
    });

  it("takes the token out of the address and keeps it nowhere", async () => {
    const view = open();
    await screen.findByLabelText(/^New password(?! again)/);
    await waitFor(() => expect(view.router()!.state.location.search).toBe(""));
    expect(view.router()!.state.location.pathname).toBe("/accounts/password-reset");
    expect(window.localStorage.length).toBe(0);
    expect(window.sessionStorage.length).toBe(0);
    expect(
      JSON.stringify(window.localStorage) + JSON.stringify(window.sessionStorage),
    ).not.toContain(SECRET);
    // The page stays on the new-password form after the address changed.
    expect(screen.getByRole("heading", { name: "Choose a new password" })).toBeInTheDocument();
  });

  it("has labelled inputs, the rule under the field, and is accessible", async () => {
    const { container } = open();
    const field = await screen.findByLabelText(/^New password(?! again)/);
    expect(field).toHaveAttribute("type", "password");
    expect(field).toHaveAttribute("autocomplete", "new-password");
    expect(field).toHaveAttribute("name", "new_password");
    const again = screen.getByLabelText(/New password again/);
    expect(again).toHaveAttribute("autocomplete", "new-password");
    expect(screen.getByText(/At least 12 characters, and not a common password/)).toBeVisible();
    await expectNoAxeViolations(container);
  });

  it("posts the token and password in the body, then goes to sign-in with a notice", async () => {
    const view = open({
      "POST /api/accounts/password-reset/complete": () => noContent(),
    });
    await userEvent.type(
      await screen.findByLabelText(/^New password(?! again)/),
      "a long new phrase",
    );
    await userEvent.type(screen.getByLabelText(/New password again/), "a long new phrase");
    await userEvent.click(screen.getByRole("button", { name: "Change password" }));
    expect(
      await screen.findByText("Password changed. Sign in with your new password."),
    ).toBeInTheDocument();
    expect(view.router()!.state.location.pathname).toBe("/accounts/sign-in");
    // the flag leaves history state, so Back or a reload doesn't repeat the notice
    await waitFor(() => expect(view.router()!.state.location.state).toBeNull());
    expect(screen.getByText("Password changed. Sign in with your new password.")).toBeVisible();
    expect(await bodies(view.fetchMock, "/api/accounts/password-reset/complete")).toEqual([
      { token: SECRET, new_password: "a long new phrase" },
    ]);
    // Nobody is signed in by this.
    expect(screen.getByRole("button", { name: "Sign in" })).toBeInTheDocument();
    for (const [r] of view.fetchMock.mock.calls) expect(r.url).not.toContain(SECRET);
  });

  it("different passwords are refused on the page, with nothing sent", async () => {
    const view = open();
    await userEvent.type(
      await screen.findByLabelText(/^New password(?! again)/),
      "a long new phrase",
    );
    await userEvent.type(screen.getByLabelText(/New password again/), "another phrase");
    await userEvent.click(screen.getByRole("button", { name: "Change password" }));
    expect(await screen.findByText(/aren't the same/)).toBeInTheDocument();
    expect(screen.getByLabelText(/New password again/)).toHaveFocus();
    expect(await bodies(view.fetchMock, "/api/accounts/password-reset/complete")).toEqual([]);
  });

  it.each([
    ["too_short", { min: "12" }, /at least 12 characters/i],
    ["too_long", { max: "128" }, /at most 128 characters/i],
    ["common", {}, /too common/i],
  ])("a %s password lands on the new password field", async (code, params, text) => {
    open({
      "POST /api/accounts/password-reset/complete": () =>
        problem(400, { code: "invalid", fields: [{ field: "new_password", code, params }] }),
    });
    await userEvent.type(await screen.findByLabelText(/^New password(?! again)/), "weak");
    await userEvent.type(screen.getByLabelText(/New password again/), "weak");
    await userEvent.click(screen.getByRole("button", { name: "Change password" }));
    await waitFor(() =>
      expect(screen.getByLabelText(/^New password(?! again)/)).toHaveAttribute(
        "aria-invalid",
        "true",
      ),
    );
    expect(screen.getAllByText(text).length).toBeGreaterThan(0);
  });

  it("an invalid token says so and offers to ask again", async () => {
    open({
      "POST /api/accounts/password-reset/complete": () =>
        problem(400, { code: "invalid", fields: [{ field: "token", code: "invalid" }] }),
    });
    await userEvent.type(
      await screen.findByLabelText(/^New password(?! again)/),
      "a long new phrase",
    );
    await userEvent.type(screen.getByLabelText(/New password again/), "a long new phrase");
    await userEvent.click(screen.getByRole("button", { name: "Change password" }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(/link isn't valid/);
    expect(screen.queryByLabelText(/^New password/)).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Ask for a new link" })).toHaveAttribute(
      "href",
      "/accounts/password-reset",
    );
  });

  it("an expired token has its own sentence", async () => {
    open({
      "POST /api/accounts/password-reset/complete": () =>
        problem(400, { code: "invalid", fields: [{ field: "token", code: "expired" }] }),
    });
    await userEvent.type(
      await screen.findByLabelText(/^New password(?! again)/),
      "a long new phrase",
    );
    await userEvent.type(screen.getByLabelText(/New password again/), "a long new phrase");
    await userEvent.click(screen.getByRole("button", { name: "Change password" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("This link has expired.");
  });

  it("asking again from the invalid state shows the request form", async () => {
    open({
      "POST /api/accounts/password-reset/complete": () =>
        problem(400, { code: "invalid", fields: [{ field: "token", code: "invalid" }] }),
    });
    await userEvent.type(
      await screen.findByLabelText(/^New password(?! again)/),
      "a long new phrase",
    );
    await userEvent.type(screen.getByLabelText(/New password again/), "a long new phrase");
    await userEvent.click(screen.getByRole("button", { name: "Change password" }));
    await userEvent.click(await screen.findByRole("link", { name: "Ask for a new link" }));
    expect(await screen.findByRole("button", { name: "Send the link" })).toBeInTheDocument();
  });

  it("an empty token is the invalid state without a request", async () => {
    const view = mountApp({ path: "/accounts/password-reset?token=", routes: signedOut });
    expect(await screen.findByRole("alert")).toHaveTextContent(/link isn't valid/);
    expect(await bodies(view.fetchMock, "/api/accounts/password-reset/complete")).toEqual([]);
  });

  it("503 read_only and a server failure are shown, not swallowed", async () => {
    open({
      "POST /api/accounts/password-reset/complete": () => problem(503, { code: "read_only" }),
    });
    await userEvent.type(
      await screen.findByLabelText(/^New password(?! again)/),
      "a long new phrase",
    );
    await userEvent.type(screen.getByLabelText(/New password again/), "a long new phrase");
    await userEvent.click(screen.getByRole("button", { name: "Change password" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/read-only/i);
  });

  it("read-only copy: the button is disabled and says why", async () => {
    mountApp({
      path: `/accounts/password-reset?token=${SECRET}`,
      routes: signedOut,
      readOnly: () => true,
    });
    const button = await screen.findByRole("button", { name: "Change password" });
    await waitFor(() => expect(button).toBeDisabled());
  });
});
