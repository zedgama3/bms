import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it } from "vitest";
import { expectNoAxeViolations } from "@/test/axe";
import { coreSets } from "@/core";
import { moduleSets } from "@/modules";
import { mountApp } from "@/test/app";
import { json, problem } from "@/test/fakeFetch";
import { setReadOnly } from "./readonly";

afterEach(() => {
  document.title = "";
});

const signedIn = {
  person_id: "7d2c2f0e-0000-4000-8000-000000000001",
  playa_name: "Dusty",
  superuser: false,
  permissions: ["core.modules.read"],
};

describe("boot", () => {
  it("renders the frame with landmarks and a skip link first in the DOM", async () => {
    const { container } = mountApp();
    expect(
      await screen.findByRole("heading", { level: 1, name: "Welcome to BMS" }),
    ).toBeInTheDocument();
    expect(screen.getAllByRole("banner")).toHaveLength(1);
    expect(screen.getAllByRole("main")).toHaveLength(1);
    expect(screen.getAllByRole("contentinfo")).toHaveLength(1);
    expect(screen.getByRole("navigation", { name: "Main" })).toBeInTheDocument();
    expect(screen.getAllByRole("heading", { level: 1 })).toHaveLength(1);
    const first = container.querySelector("a, button, input, select");
    expect(first).toHaveTextContent("Skip to content");
    expect(screen.getByRole("main")).toHaveAttribute("id", "content");
    expect(screen.getByText(/Version dev/)).toBeInTheDocument();
    await expectNoAxeViolations(container);
  });

  it("the skip link moves focus to the main region", async () => {
    mountApp();
    await screen.findByRole("heading", { level: 1 });
    await userEvent.click(screen.getByText("Skip to content"));
    expect(screen.getByRole("main")).toHaveFocus();
  });

  it("F27: a /api/modules failure shows the retry state, and retry refetches", async () => {
    let fail = true;
    const { container } = mountApp({
      routes: {
        "GET /api/modules": () =>
          fail ? problem(503, { code: "unavailable", ref: "AAAA-1111" }) : json({ modules: [] }),
      },
    });
    expect(
      await screen.findByRole("heading", { name: "Something went wrong" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("The server isn't available right now.");
    expect(screen.getByText("AAAA-1111")).toBeInTheDocument();
    await expectNoAxeViolations(container);
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("heading", { name: "Welcome to BMS" })).toBeInTheDocument();
  });

  it("F27: a network failure shows the connection sentence without a reference", async () => {
    mountApp({ routes: { "GET /api/modules": () => Promise.reject(new TypeError("offline")) } });
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Can't reach the server. Check your connection and try again.",
    );
  });

  it("F28: a 404 from the session endpoint means signed out, with no banner", async () => {
    mountApp();
    await screen.findByRole("heading", { name: "Welcome to BMS" });
    expect(screen.queryByText(/Couldn't check/)).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Sign out" })).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Sign in" })).not.toBeInTheDocument();
  });

  it("F29: a session failure shows the banner and keeps the frame, with retry", async () => {
    const { fetchMock } = mountApp({ session: "fail" });
    expect(await screen.findByText("Couldn't check whether you're signed in.")).toBeInTheDocument();
    expect(screen.getByRole("navigation", { name: "Main" })).toBeInTheDocument();
    const before = fetchMock.mock.calls.length;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(fetchMock.mock.calls.length).toBeGreaterThan(before));
  });
});

describe("home", () => {
  it("signed out: welcome and the public entries as cards", async () => {
    mountApp();
    await screen.findByRole("heading", { name: "Welcome to BMS" });
    const cards = within(screen.getByRole("main")).getByRole("link", { name: "Hello" });
    expect(cards).toHaveAttribute("href", "/hello");
  });

  it("signed in: greeting by playa name and a sign-out button", async () => {
    mountApp({ session: signedIn });
    expect(await screen.findByRole("heading", { name: "Hi, Dusty" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Sign out" })).toBeInTheDocument();
    const nav = screen.getByRole("navigation", { name: "Administration" });
    expect(within(nav).getByRole("link", { name: "Modules" })).toHaveAttribute(
      "href",
      "/admin/modules",
    );
  });

  it("signed in: an Administration group of cards under the main cards (D57)", async () => {
    mountApp({ session: signedIn });
    await screen.findByRole("heading", { name: "Hi, Dusty" });
    const main = within(screen.getByRole("main"));
    const group = main.getByRole("region", { name: "Administration" });
    expect(within(group).getByRole("heading", { level: 2, name: "Administration" })).toBeVisible();
    expect(within(group).getByRole("link", { name: "Modules" })).toHaveAttribute(
      "href",
      "/admin/modules",
    );
    // The main cards come first; the admin entry is not among them.
    const cards = main.getByRole("region", { name: "What you can do" });
    expect(within(cards).queryByRole("link", { name: "Modules" })).not.toBeInTheDocument();
    expect(cards.compareDocumentPosition(group) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("no admin entry: no Administration group and no heading (D57)", async () => {
    mountApp({ session: { ...signedIn, permissions: [] } });
    await screen.findByRole("heading", { name: "Hi, Dusty" });
    const main = within(screen.getByRole("main"));
    expect(main.queryByRole("heading", { name: "Administration" })).not.toBeInTheDocument();
    expect(main.queryByRole("region", { name: "Administration" })).not.toBeInTheDocument();
  });

  it("empty: nothing turned on", async () => {
    const { container } = mountApp({ modules: [{ name: "hello", enabled: false }] });
    expect(
      await screen.findByText("Nothing is turned on yet. An administrator can enable modules."),
    ).toBeInTheDocument();
    expect(screen.queryByRole("navigation", { name: "Main" })).not.toBeInTheDocument();
    await expectNoAxeViolations(container);
  });

  it("a disabled module's path is the not-found state", async () => {
    mountApp({ modules: [{ name: "hello", enabled: false }], path: "/hello" });
    expect(await screen.findByRole("heading", { name: "Not found" })).toBeInTheDocument();
    expect(screen.getByText("There's nothing at this address.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Go home" })).toHaveAttribute("href", "/");
  });

  it("an unknown path is the not-found state", async () => {
    mountApp({ path: "/nowhere/at/all" });
    expect(await screen.findByRole("heading", { name: "Not found" })).toBeInTheDocument();
  });
});

describe("banners", () => {
  it("the read-only banner follows X-Read-Only both ways", async () => {
    let ro = true;
    const { client } = mountApp({ readOnly: () => ro });
    expect(await screen.findByText(/This copy is read-only/)).toBeInTheDocument();
    ro = false;
    await act(async () => {
      await client.refetchQueries({ queryKey: ["core", "modules"] });
    });
    await waitFor(() =>
      expect(screen.queryByText(/This copy is read-only/)).not.toBeInTheDocument(),
    );
  });

  it("the read-only banner is a status region", async () => {
    mountApp();
    await screen.findByRole("heading", { level: 1 });
    act(() => setReadOnly(true));
    const banner = await screen.findByText(/This copy is read-only. You can look things up/);
    expect(banner.closest('[role="status"]')).not.toBeNull();
  });

  it("the superuser banner shows for a superuser session", async () => {
    mountApp({ session: { ...signedIn, superuser: true } });
    expect(
      await screen.findByText(
        "You are signed in as the break-glass superuser. Every action is recorded.",
      ),
    ).toBeInTheDocument();
  });
});

describe("navigation", () => {
  it("F32: a route change moves focus to the h1, sets the title and announces", async () => {
    mountApp();
    await screen.findByRole("heading", { name: "Welcome to BMS" });
    await waitFor(() => expect(document.title).toBe("Home · BMS"));
    await userEvent.click(
      within(screen.getByRole("navigation", { name: "Main" })).getByRole("link", { name: "Hello" }),
    );
    const h1 = await screen.findByRole("heading", { level: 1, name: /Hello/ });
    await waitFor(() => expect(h1).toHaveFocus());
    await waitFor(() => expect(document.title).toBe("Hello · BMS"));
    expect(screen.getAllByRole("status").some((s) => s.textContent === "Hello")).toBe(true);
  });

  it("a long bar keeps every label whole: links are nowrap from md and the lists wrap by item", async () => {
    const permissions: string[] = [];
    for (const set of [...coreSets, ...moduleSets])
      for (const n of set.nav) if (typeof n.access === "object") permissions.push(...n.access);
    mountApp({ session: { ...signedIn, permissions } });
    await screen.findByRole("heading", { level: 1 });
    const links = [
      ...within(screen.getByRole("navigation", { name: "Main" })).getAllByRole("link"),
      ...within(screen.getByRole("navigation", { name: "Administration" })).getAllByRole("link"),
    ];
    expect(links.length).toBeGreaterThanOrEqual(11);
    for (const a of links)
      expect(a.className, a.textContent ?? "").toContain("md:whitespace-nowrap");
    for (const nav of screen.getAllByRole("navigation")) {
      expect(nav.querySelector("ul")?.className).toContain("md:flex-wrap");
    }
    // the person block is not squeezed: it is its own non-shrinking item on wide screens
    const signOut = screen.getByRole("button", { name: "Sign out" });
    expect(signOut.className).toContain("whitespace-nowrap");
    expect(signOut.className).toContain("shrink-0");
    // ...while the name may wrap between words
    expect(screen.getByText("Dusty").className).toContain("min-w-0");
  });

  it("the mobile menu button toggles aria-expanded and closes on navigation", async () => {
    mountApp();
    await screen.findByRole("heading", { level: 1 });
    const menu = screen.getByRole("button", { name: "Menu" });
    expect(menu).toHaveAttribute("aria-expanded", "false");
    const panel = document.getElementById(menu.getAttribute("aria-controls") ?? "")!;
    expect(panel.className).toContain("hidden");
    await userEvent.click(menu);
    expect(menu).toHaveAttribute("aria-expanded", "true");
    expect(panel.className).not.toMatch(/(^| )hidden( |$)/);
    await userEvent.click(within(panel).getByRole("link", { name: "Hello" }));
    await screen.findByRole("heading", { level: 1, name: /Hello/ });
    expect(screen.getByRole("button", { name: "Menu" })).toHaveAttribute("aria-expanded", "false");
  });

  it("F35: a signed-in route visited signed out redirects to sign-in before any call", async () => {
    const { fetchMock, router } = mountApp({ path: "/admin/modules" });
    await screen.findByRole("heading", { name: "Sign in" });
    expect(router()!.state.location.pathname).toBe("/accounts/sign-in");
    expect(router()!.state.location.search).toBe("?returnTo=%2Fadmin%2Fmodules");
    expect(
      fetchMock.mock.calls.some(([r]) => new URL(r.url).pathname === "/api/admin/modules"),
    ).toBe(false);
  });
});

describe("sign out", () => {
  it("clears the query cache and the session, and leaves the page", async () => {
    const { client, leave } = mountApp({
      session: signedIn,
      routes: { "POST /api/accounts/sign-out": () => new Response(null, { status: 204 }) },
    });
    await screen.findByRole("heading", { name: "Hi, Dusty" });
    expect(client.getQueryCache().getAll().length).toBeGreaterThan(0);
    await userEvent.click(screen.getByRole("button", { name: "Sign out" }));
    await waitFor(() => expect(leave).toHaveBeenCalledWith("/"));
    // Nothing the old session fetched stays: a disabled, empty query may remain, data may not.
    expect(
      client
        .getQueryCache()
        .getAll()
        .filter((q) => q.state.data !== undefined),
    ).toHaveLength(0);
  });
});
