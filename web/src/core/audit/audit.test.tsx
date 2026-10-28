import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { expectNoAxeViolations } from "@/test/axe";
import { mountApp } from "@/test/app";
import { json, problem } from "@/test/fakeFetch";

const ME = "7d2c2f0e-0000-4000-8000-000000000001";
const PAT = "7d2c2f0e-0000-4000-8000-000000000009";
const ident = (permissions: string[]) => ({
  person_id: ME,
  account_id: "0b6f1c3e-0000-4000-8000-000000000002",
  playa_name: "Dusty",
  superuser: false,
  password_change_required: false,
  expires_at: "2099-01-01T00:00:00Z",
  signup_open: true,
  permissions,
});
const record = (over: Record<string, unknown> = {}) => ({
  v: 1,
  id: "0b6f1c3e-0000-4000-8000-0000000000f1",
  at: "2026-10-05T10:00:00Z",
  instance: "i",
  action: "people.person.read",
  outcome: "succeeded",
  actor: { kind: "person", person: ME, titles: ["Access administrator"] },
  subject: PAT,
  level: "personal",
  fields: ["phone", "legal_name"],
  record: { kind: "people.person", id: PAT },
  ...over,
});
type Routes = Record<string, (req: Request) => Response | Promise<Response>>;
const base = (over: Routes = {}): Routes => ({
  "GET /api/audit/entries": () => json({ records: [record()] }),
  "GET /api/people/names": () => json({ names: { [PAT]: "Pat Dust", [ME]: "Dusty" } }),
  "GET /api/people/me": () => problem(404, { code: "not_found" }),
  ...over,
});
afterEach(() => {
  Reflect.deleteProperty(URL, "createObjectURL");
  Reflect.deleteProperty(URL, "revokeObjectURL");
});

describe("/audit", () => {
  it("lists entries as cards with names; the menu entry shows to a holder; accessible", async () => {
    const { container } = mountApp({
      path: "/audit",
      session: ident(["audit.search"]),
      routes: base(),
    });
    expect(await screen.findByText("people.person.read")).toBeInTheDocument();
    expect(await screen.findByText("About Pat Dust")).toBeInTheDocument();
    expect(await screen.findByText("By Dusty (Access administrator)")).toBeInTheDocument();
    expect(screen.getByText("Fields: phone and legal_name")).toBeInTheDocument();
    expect(
      within(screen.getByRole("navigation", { name: "Administration" })).getByRole("link", {
        name: "Audit log",
      }),
    ).toHaveAttribute("href", "/audit");
    await expectNoAxeViolations(container);
  });

  it("a person without a playa name shows 'No playa name yet', never an ID", async () => {
    mountApp({
      path: "/audit",
      session: ident(["audit.search"]),
      routes: base({ "GET /api/people/names": () => json({ names: { [PAT]: "", [ME]: "" } }) }),
    });
    expect(await screen.findByText("About No playa name yet")).toBeInTheDocument();
    expect(screen.getByText("By No playa name yet (Access administrator)")).toBeInTheDocument();
    expect(screen.queryByText(/^About 7d2c/)).not.toBeInTheDocument();
  });

  it("an ID the names answer omits, or a failed names call, shows the ID, not 'unnamed'", async () => {
    for (const names of [() => json({ names: {} }), () => problem(500, { code: "internal" })]) {
      const { unmount } = mountApp({
        path: "/audit",
        session: ident(["audit.search"]),
        routes: base({ "GET /api/people/names": names }),
      });
      expect(await screen.findByText(`About ${PAT}`)).toBeInTheDocument();
      expect(screen.getByText(`By ${ME} (Access administrator)`)).toBeInTheDocument();
      expect(screen.queryByText(/No playa name yet/)).not.toBeInTheDocument();
      unmount();
    }
  });

  it("does not call a named person unnamed while the names are still loading", async () => {
    let release: () => void = () => {};
    const gate = new Promise<void>((r) => {
      release = r;
    });
    mountApp({
      path: "/audit",
      session: ident(["audit.search"]),
      routes: base({
        "GET /api/people/names": async () => {
          await gate;
          return json({ names: { [PAT]: "Pat Dust", [ME]: "Dusty" } });
        },
      }),
    });
    expect(await screen.findByText("people.person.read")).toBeInTheDocument();
    expect(screen.queryByText(/No playa name yet/)).not.toBeInTheDocument();
    release();
    expect(await screen.findByText("About Pat Dust")).toBeInTheDocument();
    expect(screen.queryByText(/No playa name yet/)).not.toBeInTheDocument();
  });

  it("?subject= from the person page filters, shows the name and offers the export", async () => {
    const { fetchMock } = mountApp({
      path: `/audit?subject=${PAT}`,
      session: ident(["audit.search", "audit.export"]),
      routes: base(),
    });
    expect(await screen.findByText("Pat Dust", { selector: "span" })).toBeInTheDocument();
    const call = fetchMock.mock.calls.find(
      ([r]) => new URL(r.url).pathname === "/api/audit/entries",
    );
    expect(new URL(call![0].url).searchParams.get("subject")).toBe(PAT);
    expect(screen.getByRole("button", { name: "Export views" })).toBeInTheDocument();
    expect(screen.getByText("The export is itself recorded in the audit log.")).toBeInTheDocument();
  });

  it("no export button without audit.export or without a subject", async () => {
    mountApp({ path: `/audit?subject=${PAT}`, session: ident(["audit.search"]), routes: base() });
    await screen.findByText("people.person.read");
    expect(screen.queryByRole("button", { name: "Export views" })).not.toBeInTheDocument();
  });

  it("downloads the JSON Lines export as a .jsonl file, many lines and all", async () => {
    Object.defineProperty(URL, "createObjectURL", { configurable: true, value: () => "blob:x" });
    Object.defineProperty(URL, "revokeObjectURL", { configurable: true, value: () => {} });
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
    mountApp({
      path: `/audit?subject=${PAT}`,
      session: ident(["audit.search", "audit.export"]),
      routes: base({
        [`GET /api/audit/people/${PAT}/views`]: () =>
          new Response('{"id":"a"}\n{"id":"b"}\n', {
            headers: { "Content-Type": "application/x-ndjson" },
          }),
      }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Export views" }));
    await screen.findByText("The export was downloaded");
    expect(click).toHaveBeenCalled();
    click.mockRestore();
  });

  it("applies filters to the address and the request; both dates are needed", async () => {
    const { fetchMock, router } = mountApp({
      path: "/audit",
      session: ident(["audit.search"]),
      routes: base(),
    });
    await screen.findByText("people.person.read");
    await userEvent.type(screen.getByLabelText("Area"), "people");
    await userEvent.type(screen.getByLabelText("From (first day)"), "2026-10-01");
    await userEvent.click(screen.getByRole("button", { name: "Apply filters" }));
    expect((await screen.findAllByText("Give both dates, or neither.")).length).toBeGreaterThan(0);
    await userEvent.type(screen.getByLabelText("To (last day)"), "2026-10-02");
    await userEvent.click(screen.getByRole("button", { name: "Apply filters" }));
    await waitFor(() => expect(router()!.state.location.search).toContain("area=people"));
    const last = fetchMock.mock.calls
      .filter(([r]) => new URL(r.url).pathname === "/api/audit/entries")
      .at(-1)!;
    const sp = new URL(last[0].url).searchParams;
    expect(sp.get("area")).toBe("people");
    expect(sp.get("from")).toBe(new Date(2026, 9, 1).toISOString());
    expect(sp.get("to")).toBe(new Date(2026, 9, 3).toISOString());
  });

  it("pages by cursor, and says when nothing matches", async () => {
    const { unmount } = mountApp({
      path: "/audit",
      session: ident(["audit.search"]),
      routes: base({
        "GET /api/audit/entries": (r) => {
          return new URL(r.url).searchParams.get("after")
            ? json({ records: [record({ id: "x2", action: "people.person.changed" })] })
            : json({ records: [record()], next: "cur" });
        },
      }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Show more" }));
    expect(await screen.findByText("people.person.changed")).toBeInTheDocument();
    unmount();
    mountApp({
      path: "/audit",
      session: ident(["audit.search"]),
      routes: base({ "GET /api/audit/entries": () => json({ records: [] }) }),
    });
    expect(await screen.findByText("No audit entries match these filters.")).toBeInTheDocument();
  });

  it("without audit.search the forbidden state shows and nothing is called", async () => {
    const { fetchMock } = mountApp({ path: "/audit", session: ident([]), routes: base() });
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "You don't have access to this screen.",
    );
    expect(
      fetchMock.mock.calls.some(([r]) => new URL(r.url).pathname === "/api/audit/entries"),
    ).toBe(false);
  });

  it("403, 429 and 500 reads are shown; a bad filter lands in the summary", async () => {
    for (const [status, code, words] of [
      [403, "permission", "You don't have access"],
      [429, "rate_limited", "Too many attempts"],
      [500, "internal", "Something went wrong"],
    ] as const) {
      const m = mountApp({
        path: "/audit",
        session: ident(["audit.search"]),
        routes: base({ "GET /api/audit/entries": () => problem(status, { code }) }),
      });
      expect(await screen.findByRole("alert")).toHaveTextContent(words);
      m.unmount();
    }
    mountApp({
      path: "/audit?record_id=0b6f1c3e-0000-4000-8000-0000000000f1",
      session: ident(["audit.search"]),
      routes: base({
        "GET /api/audit/entries": () =>
          problem(400, {
            code: "invalid",
            fields: [{ field: "query.record_kind", code: "required", reason: "x" }],
          }),
      }),
    });
    await screen.findByRole("alert");
    expect(screen.getByLabelText("Record kind")).toHaveAttribute("aria-invalid", "true");
  });

  it("works on a read-only copy", async () => {
    mountApp({
      path: "/audit",
      session: ident(["audit.search"]),
      routes: base(),
      readOnly: () => true,
    });
    expect(await screen.findByText("people.person.read")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Apply filters" })).toBeEnabled();
  });
});

describe("review fixes", () => {
  it("typed filters survive applying another filter and focus moves to the count line", async () => {
    mountApp({ path: "/audit", session: ident(["audit.search"]), routes: base() });
    await screen.findByText("people.person.read", { selector: "p.text-lg" });
    await userEvent.type(screen.getByLabelText("Record kind"), "people.person");
    await userEvent.type(screen.getByLabelText("Area"), "people");
    await userEvent.click(screen.getByRole("button", { name: "Apply filters" }));
    await waitFor(() => expect(screen.getByText("1 entry shown")).toHaveFocus());
    expect(screen.getByLabelText("Record kind")).toHaveValue("people.person");
    await userEvent.type(screen.getByLabelText("Action"), "x");
    await userEvent.click(screen.getByRole("button", { name: "Apply filters" }));
    expect(screen.getByLabelText("Action")).toHaveValue("x");
    await userEvent.click(screen.getByRole("button", { name: "Clear filters" }));
    expect(screen.getByLabelText("Area")).toHaveValue("");
  });

  it.each([
    ["query.action", "mismatch", "This action isn't in that area."],
    ["query.from", "range", "The first day must be on or before the last day."],
  ])("%s %s is translated and the input is linked from the summary", async (field, code, words) => {
    mountApp({
      path: "/audit",
      session: ident(["audit.search"]),
      routes: base({
        "GET /api/audit/entries": () =>
          problem(400, { code: "invalid", fields: [{ field, code, reason: "x" }] }),
      }),
    });
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(words);
    expect(within(alert).getByRole("link")).toBeInTheDocument();
  });

  it("an actor's role title is translated when the catalogue knows it and listed with 'and'", async () => {
    mountApp({
      path: "/audit",
      session: ident(["audit.search"]),
      routes: base({
        "GET /api/audit/entries": () =>
          json({
            records: [
              record({
                actor: { kind: "person", person: ME, titles: ["access-admin", "Gate crew"] },
              }),
            ],
          }),
      }),
    });
    expect(
      await screen.findByText("By Dusty (Access administrator and Gate crew)"),
    ).toBeInTheDocument();
  });
});
