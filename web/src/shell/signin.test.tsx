import { act, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { safeReturnTo } from "./boot";
import { expectNoAxeViolations } from "@/test/axe";
import { mountApp } from "@/test/app";
import { json, problem } from "@/test/fakeFetch";
import { getSession } from "./session";

const ident = {
  person_id: "7d2c2f0e-0000-4000-8000-000000000001",
  playa_name: "Dusty",
  superuser: false,
  permissions: [],
};

/** A server where identity exists: the session is signed out until sign-in succeeds. */
function identityRoutes(over: Record<string, () => Response> = {}) {
  let signedIn = false;
  return {
    routes: {
      "GET /api/accounts/session": () =>
        signedIn ? json({ signed_in: true, ...ident }) : json({ signed_in: false }),
      "POST /api/accounts/sign-in": () => {
        signedIn = true;
        return new Response(null, { status: 204 });
      },
      ...over,
    },
  };
}

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

describe("/sign-in", () => {
  it("F28: in M0 it says sign-in isn't available, and there is no form", async () => {
    const { container } = mountApp({ path: "/sign-in" });
    expect(await screen.findByRole("heading", { level: 1, name: "Sign in" })).toBeInTheDocument();
    expect(screen.getByText("Sign-in isn't available in this build yet.")).toBeInTheDocument();
    expect(screen.queryByLabelText(/Password/)).not.toBeInTheDocument();
    await expectNoAxeViolations(container);
  });

  it("the form has labelled e-mail and password inputs with the right type, inputmode and autocomplete", async () => {
    const { container } = mountApp({ path: "/sign-in", ...identityRoutes() });
    const email = await screen.findByLabelText(/E-mail address/);
    const password = screen.getByLabelText(/Password/);
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

  it("signs in, refreshes the session and follows returnTo", async () => {
    const { router, fetchMock } = mountApp({
      path: "/sign-in?returnTo=%2Fhello",
      ...identityRoutes(),
    });
    await userEvent.type(await screen.findByLabelText(/E-mail address/), "pat@example.test");
    await userEvent.type(screen.getByLabelText(/Password/), "correct horse");
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    await waitFor(() => expect(router()!.state.location.pathname).toBe("/hello"));
    expect(getSession()).toMatchObject({ signedIn: true, playaName: "Dusty" });
    const signIn = fetchMock.mock.calls.find(
      ([r]) => new URL(r.url).pathname === "/api/accounts/sign-in",
    )![0];
    expect(await signIn.json()).toEqual({ email: "pat@example.test", password: "correct horse" });
  });

  it("F34: an open redirect is refused; the person lands on /", async () => {
    const { router } = mountApp({
      path: "/sign-in?returnTo=%2F%2Fevil.example",
      ...identityRoutes(),
    });
    await userEvent.type(await screen.findByLabelText(/E-mail address/), "a@b.test");
    await userEvent.type(screen.getByLabelText(/Password/), "x");
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    await waitFor(() => expect(router()!.state.location.pathname).toBe("/"));
  });

  it("401 says the e-mail address or password isn't right, never which, and does not redirect", async () => {
    const { router } = mountApp({
      path: "/sign-in",
      ...identityRoutes({
        "POST /api/accounts/sign-in": () => problem(401, { code: "unauthenticated" }),
      }),
    });
    await userEvent.type(await screen.findByLabelText(/E-mail address/), "a@b.test");
    await userEvent.type(screen.getByLabelText(/Password/), "x");
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "That e-mail address or password isn't right.",
    );
    expect(router()!.state.location.pathname).toBe("/sign-in");
    expect(screen.getByRole("button", { name: "Sign in" })).not.toBeDisabled();
  });

  it("429 shows the rate-limit sentence with the seconds", async () => {
    mountApp({
      path: "/sign-in",
      ...identityRoutes({
        "POST /api/accounts/sign-in": () =>
          problem(429, { code: "rate_limited" }, { "Retry-After": "20" }),
      }),
    });
    await userEvent.type(await screen.findByLabelText(/E-mail address/), "a@b.test");
    await userEvent.type(screen.getByLabelText(/Password/), "x");
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Wait 20 seconds and try again.");
  });

  it("a field error lands on the input named by fields[].field", async () => {
    mountApp({
      path: "/sign-in",
      ...identityRoutes({
        "POST /api/accounts/sign-in": () =>
          problem(400, {
            code: "invalid",
            fields: [{ field: "email", code: "malformed", reason: "x" }],
          }),
      }),
    });
    await userEvent.type(await screen.findByLabelText(/E-mail address/), "nope");
    await userEvent.type(screen.getByLabelText(/Password/), "x");
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    await screen.findByRole("alert");
    expect(screen.getByLabelText(/E-mail address/)).toHaveAttribute("aria-invalid", "true");
    expect(screen.getAllByText(/This isn't in the expected form\./).length).toBeGreaterThan(0);
  });

  it("a signed-in person visiting sign-in is sent on", async () => {
    const { router } = mountApp({ path: "/sign-in?returnTo=%2Fhello", session: ident });
    await waitFor(() => expect(router()!.state.location.pathname).toBe("/hello"));
  });

  it("a 401 elsewhere clears the session and cache, goes to sign-in with returnTo, and toasts", async () => {
    const { router, client } = mountApp({
      path: "/admin/modules",
      session: ident,
      routes: { "GET /api/admin/modules": () => problem(401, { code: "unauthenticated" }) },
    });
    await waitFor(() => expect(router()!.state.location.pathname).toBe("/sign-in"));
    expect(router()!.state.location.search).toBe("?returnTo=%2Fadmin%2Fmodules");
    expect(getSession()).toEqual({ signedIn: false });
    expect(await screen.findByText("Your session ended. Sign in again.")).toBeInTheDocument();
    expect(client.getQueryCache().find({ queryKey: ["core", "admin-modules"] })).toBeUndefined();
    await act(async () => {});
  });
});
