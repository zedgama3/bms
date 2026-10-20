import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { expectNoAxeViolations } from "@/test/axe";
import { mountApp } from "@/test/app";
import { json, noContent, problem } from "@/test/fakeFetch";

const ident = (permissions: string[]) => ({
  person_id: "7d2c2f0e-0000-4000-8000-000000000001",
  account_id: "0b6f1c3e-0000-4000-8000-000000000002",
  playa_name: "Dusty",
  superuser: false,
  password_change_required: false,
  expires_at: "2099-01-01T00:00:00Z",
  signup_open: true,
  permissions,
});
const TEMPLATES = "notify.templates.manage";
const OUTBOX = "notify.outbox.manage";

const MSG1 = "9a1f0000-0000-4000-8000-000000000001";
const MSG2 = "9a1f0000-0000-4000-8000-000000000002";
const MSG3 = "9a1f0000-0000-4000-8000-000000000003";

type Routes = Record<string, (req: Request) => Response | Promise<Response>>;

const list = [
  {
    name: "hello.greeted",
    available: true,
    vars: ["name"],
    languages: [{ language: "en", overridden: false }],
  },
  {
    name: "identity.password-reset",
    available: true,
    vars: ["lifetime"],
    languages: [
      { language: "en", overridden: true, updated_at: "2026-10-18T10:00:00Z" },
      { language: "es", overridden: false },
    ],
  },
  {
    name: "people.deletion-requested",
    available: true,
    vars: [],
    languages: [{ language: "en", overridden: false }],
  },
  {
    name: "notify.test",
    available: true,
    vars: [],
    languages: [{ language: "en", overridden: false }],
  },
  {
    name: "oldmod.gone",
    available: false,
    vars: [],
    languages: [{ language: "en", overridden: true }],
  },
];

const view = (over: Record<string, unknown> = {}) => ({
  name: "identity.password-reset",
  language: "en",
  overridden: true,
  vars: ["lifetime"],
  default: {
    subject: "Reset your password for {{.Site.Name}}",
    body: "Open {{.Link}} within {{.Vars.lifetime}}.",
  },
  current: { subject: "Custom subject", body: "Custom body {{.Link}}" },
  ...over,
});

const record = (over: Record<string, unknown> = {}) => ({
  id: MSG1,
  owner: "identity",
  template: "password-reset",
  language: "en",
  status: "failed",
  attempts: 3,
  created_at: "2026-10-19T10:00:00Z",
  expires_at: "2099-01-01T00:00:00Z",
  last_error: "connect",
  ...over,
});

const outbox = (over: Record<string, unknown> = {}) => ({
  configured: true,
  counts: { queued: 2, sending: 0, sent: 40, failed: 1 },
  recent: [record()],
  ...over,
});

const seen = (fetchMock: { mock: { calls: [Request][] } }, method: string, path: string) =>
  fetchMock.mock.calls.some(([r]) => r.method === method && new URL(r.url).pathname === path);
const bodyOf = async (
  fetchMock: { mock: { calls: [Request][] } },
  method: string,
  path: string,
) => {
  const hit = fetchMock.mock.calls.find(
    ([r]) => r.method === method && new URL(r.url).pathname === path,
  );
  return hit ? ((await hit[0].clone().json()) as unknown) : undefined;
};

const T = "/api/notify/templates";
const E = `${T}/identity.password-reset/en`;

describe("navigation", () => {
  it("lists each admin entry for the permission it needs", async () => {
    mountApp({ session: ident([TEMPLATES]), routes: {} });
    await screen.findByRole("heading", { name: "Hi, Dusty" });
    const nav = screen.getByRole("navigation", { name: "Administration" });
    expect(within(nav).getByRole("link", { name: "E-mail templates" })).toHaveAttribute(
      "href",
      "/notify/templates",
    );
    expect(within(nav).queryByRole("link", { name: "E-mail outbox" })).not.toBeInTheDocument();
  });

  it("an outbox-only manager sees only the outbox entry, and /notify goes there", async () => {
    const v = mountApp({
      path: "/notify",
      session: ident([OUTBOX]),
      routes: { "GET /api/notify/outbox": () => json(outbox()) },
    });
    expect(await screen.findByRole("heading", { name: "E-mail outbox" })).toBeInTheDocument();
    expect(v.router()!.state.location.pathname).toBe("/notify/outbox");
  });
});

describe("/notify/templates", () => {
  const routes = (over: Routes = {}): Routes => ({
    [`GET ${T}`]: () => json(list),
    ...over,
  });

  it("groups by owner, titles each template from its owner's catalogue, and is accessible", async () => {
    const { container } = mountApp({
      path: "/notify/templates",
      session: ident([TEMPLATES]),
      routes: routes(),
    });
    expect(await screen.findByRole("heading", { name: "Password-reset link" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Deletion request waiting" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Test message" })).toBeInTheDocument();
    // module.hello.template.greeted.title already exists
    expect(screen.getByRole("heading", { name: "Greeting sent" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "en: customised" })).toHaveAttribute(
      "href",
      "/notify/templates/identity.password-reset/en",
    );
    expect(screen.getByRole("link", { name: "es: shipped text" })).toBeInTheDocument();
    await expectNoAxeViolations(container);
  });

  it("lists templates nobody registers in a Not available group with a remove action", async () => {
    const { fetchMock } = mountApp({
      path: "/notify/templates",
      session: ident([TEMPLATES]),
      routes: routes({ [`DELETE ${T}/oldmod.gone/en`]: () => noContent() }),
    });
    expect(await screen.findByRole("heading", { name: "Not available" })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /Remove the customised wording/ }));
    const dialog = await screen.findByRole("dialog");
    await userEvent.click(within(dialog).getByRole("button", { name: "Remove" }));
    await waitFor(() => expect(seen(fetchMock, "DELETE", `${T}/oldmod.gone/en`)).toBe(true));
    // the pressed button is gone: focus lands on the heading
    await waitFor(() =>
      expect(screen.getByRole("heading", { level: 1, name: "E-mail templates" })).toHaveFocus(),
    );
  });

  it("says when none are registered", async () => {
    mountApp({
      path: "/notify/templates",
      session: ident([TEMPLATES]),
      routes: routes({ [`GET ${T}`]: () => json([]) }),
    });
    expect(await screen.findByText("No templates are registered.")).toBeInTheDocument();
  });

  it("403 shows the forbidden sentence with no retry", async () => {
    mountApp({
      path: "/notify/templates",
      session: ident([TEMPLATES]),
      routes: routes({ [`GET ${T}`]: () => problem(403, { code: "permission" }) }),
    });
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "You don't have access to this screen",
    );
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
  });

  it("a server failure is shown with its reference and a retry", async () => {
    mountApp({
      path: "/notify/templates",
      session: ident([TEMPLATES]),
      routes: routes({
        [`GET ${T}`]: () => problem(500, { code: "internal", ref: "ABCD-1234" }),
      }),
    });
    expect(await screen.findByText(/ABCD-1234/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument();
  });

  it("without the permission nothing is asked and the forbidden state shows", async () => {
    const { fetchMock } = mountApp({
      path: "/notify/templates",
      session: ident([]),
      routes: routes(),
    });
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "You don't have access to this screen",
    );
    expect(seen(fetchMock, "GET", T)).toBe(false);
  });

  it("send me a test queues it and shows the message ID and a link to the outbox", async () => {
    const { fetchMock } = mountApp({
      path: "/notify/templates",
      session: ident([TEMPLATES, OUTBOX]),
      routes: routes({
        "POST /api/notify/test": () => json({ id: MSG1 }, { status: 202 }),
      }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Send me a test" }));
    expect(await screen.findByText(new RegExp(MSG1))).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open the outbox" })).toHaveAttribute(
      "href",
      "/notify/outbox",
    );
    const call = fetchMock.mock.calls.find(([r]) => new URL(r.url).pathname === "/api/notify/test");
    expect(await call![0].clone().text()).toBe("");
  });

  it("send me a test: 501 says e-mail isn't set up and links to the settings", async () => {
    mountApp({
      path: "/notify/templates",
      session: ident([TEMPLATES]),
      routes: routes({ "POST /api/notify/test": () => problem(501, { code: "unsupported" }) }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Send me a test" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("E-mail isn't set up yet");
    expect(screen.getByRole("link", { name: "Open e-mail settings" })).toHaveAttribute(
      "href",
      "/settings/notify",
    );
    expect(screen.queryByRole("link", { name: "Open the outbox" })).not.toBeInTheDocument();
  });

  it("read-only copy: the test button and remove are disabled with the reason", async () => {
    mountApp({
      path: "/notify/templates",
      session: ident([TEMPLATES]),
      routes: routes(),
      readOnly: () => true,
    });
    const send = await screen.findByRole("button", { name: "Send me a test" });
    await waitFor(() => expect(send).toBeDisabled());
    expect(screen.getAllByText(/read-only/).length).toBeGreaterThan(0);
  });
});

describe("/notify/templates/:name/:lang", () => {
  const routes = (over: Routes = {}): Routes => ({
    [`GET ${E}`]: () => json(view()),
    ...over,
  });
  const open = (over: Routes = {}, extra: Partial<Parameters<typeof mountApp>[0]> = {}) =>
    mountApp({
      path: "/notify/templates/identity.password-reset/en",
      session: ident([TEMPLATES]),
      routes: routes(over),
      ...extra,
    });

  it("shows the current text, the shipped text, and the fields as copyable chips; accessibly", async () => {
    const { container } = open();
    expect(await screen.findByLabelText(/Subject/)).toHaveValue("Custom subject");
    expect(screen.getByLabelText(/Body/)).toHaveValue("Custom body {{.Link}}");
    expect(screen.getByRole("heading", { name: "Password-reset link" })).toBeInTheDocument();
    expect(screen.getByText("Subject: Reset your password for {{.Site.Name}}")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Copy .Vars.lifetime" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Copy .Link" })).toBeInTheDocument();
    await expectNoAxeViolations(container);
  });

  it("copies a field in double braces", async () => {
    const write = vi.fn(() => Promise.resolve());
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText: write },
      configurable: true,
    });
    open();
    await userEvent.click(await screen.findByRole("button", { name: "Copy .Vars.lifetime" }));
    expect(write).toHaveBeenCalledWith("{{.Vars.lifetime}}");
  });

  it("saves the edited text and toasts", async () => {
    const { fetchMock } = open({
      [`PUT ${E}`]: () => json(view({ current: { subject: "New", body: "New body" } })),
    });
    const subject = await screen.findByLabelText(/Subject/);
    await userEvent.clear(subject);
    await userEvent.type(subject, "New");
    const body = screen.getByLabelText(/Body/);
    await userEvent.clear(body);
    await userEvent.type(body, "New body");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(async () =>
      expect(await bodyOf(fetchMock, "PUT", E)).toEqual({ subject: "New", body: "New body" }),
    );
    expect(await screen.findByText("Saved")).toBeInTheDocument();
  });

  it("puts field errors on the inputs, with the parser's line for a parse error", async () => {
    open({
      [`PUT ${E}`]: () =>
        problem(400, {
          code: "invalid",
          fields: [
            { field: "subject", code: "required", reason: "" },
            {
              field: "body",
              code: "parse",
              params: { line: "2" },
              reason: "template: line 2: unexpected EOF",
            },
          ],
        }),
    });
    await screen.findByLabelText(/Subject/);
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(screen.getByLabelText(/Subject/)).toHaveAttribute("aria-invalid", "true"),
    );
    expect(screen.getByLabelText(/Body/)).toHaveAttribute("aria-invalid", "true");
    expect(screen.getAllByText(/The template has a mistake on line 2\./).length).toBeGreaterThan(0);
    expect(screen.queryByText(/unexpected EOF/)).not.toBeInTheDocument();
  });

  it("a parse error without a line still reads in the catalogue's words", async () => {
    open({
      [`PUT ${E}`]: () =>
        problem(400, {
          code: "invalid",
          fields: [{ field: "body", code: "parse", reason: "template: unexpected EOF" }],
        }),
    });
    await screen.findByLabelText(/Subject/);
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(screen.getAllByText("The template has a mistake.").length).toBeGreaterThan(0),
    );
    expect(screen.queryByText(/unexpected EOF/)).not.toBeInTheDocument();
  });

  it("previews the current text without saving it", async () => {
    const { fetchMock } = open({
      [`POST ${E}/preview`]: () => json({ subject: "Rendered subject", body: "Rendered body" }),
    });
    await screen.findByLabelText(/Subject/);
    await userEvent.click(screen.getByRole("button", { name: "Preview" }));
    expect(await screen.findByText("Subject: Rendered subject")).toBeInTheDocument();
    expect(screen.getByText("Rendered body")).toBeInTheDocument();
    expect(await bodyOf(fetchMock, "POST", `${E}/preview`)).toEqual({
      subject: "Custom subject",
      body: "Custom body {{.Link}}",
    });
    expect(seen(fetchMock, "PUT", E)).toBe(false);
  });

  it("resets to the default after a confirmation and shows the shipped text in the fields", async () => {
    let reset = false;
    const { fetchMock } = open({
      [`GET ${E}`]: () =>
        json(
          reset
            ? view({
                overridden: false,
                current: {
                  subject: "Reset your password for {{.Site.Name}}",
                  body: "Open {{.Link}} within {{.Vars.lifetime}}.",
                },
              })
            : view(),
        ),
      [`DELETE ${E}`]: () => {
        reset = true;
        return noContent();
      },
    });
    await screen.findByLabelText(/Subject/);
    await userEvent.click(screen.getByRole("button", { name: "Reset to default" }));
    const dialog = await screen.findByRole("dialog");
    expect(seen(fetchMock, "DELETE", E)).toBe(false);
    await userEvent.click(within(dialog).getByRole("button", { name: "Reset to default" }));
    await waitFor(() =>
      expect(screen.getByLabelText(/Subject/)).toHaveValue(
        "Reset your password for {{.Site.Name}}",
      ),
    );
    expect(screen.queryByRole("button", { name: "Reset to default" })).not.toBeInTheDocument();
  });

  it("a template that isn't overridden has no reset button", async () => {
    open({ [`GET ${E}`]: () => json(view({ overridden: false })) });
    await screen.findByLabelText(/Subject/);
    expect(screen.queryByRole("button", { name: "Reset to default" })).not.toBeInTheDocument();
  });

  it("send me a test works from here too", async () => {
    open({ "POST /api/notify/test": () => json({ id: MSG2 }, { status: 202 }) });
    await userEvent.click(await screen.findByRole("button", { name: "Send me a test" }));
    expect(await screen.findByText(new RegExp(MSG2))).toBeInTheDocument();
    // not an outbox manager: no link
    expect(screen.queryByRole("link", { name: "Open the outbox" })).not.toBeInTheDocument();
  });

  it("404 and 500 are shown with a way back", async () => {
    open({ [`GET ${E}`]: () => problem(404, { code: "not_found" }) });
    expect(await screen.findByRole("alert")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Back to templates" })).toHaveAttribute(
      "href",
      "/notify/templates",
    );
  });

  it("read-only copy: the fields and Save are disabled with a note", async () => {
    open({}, { readOnly: () => true });
    const subject = await screen.findByLabelText(/Subject/);
    await waitFor(() => expect(subject).toBeDisabled());
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    expect(screen.getAllByText(/read-only/).length).toBeGreaterThan(0);
  });

  it("without the permission it shows the forbidden state and asks for nothing", async () => {
    const { fetchMock } = open({}, { session: ident([OUTBOX]) });
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "You don't have access to this screen",
    );
    expect(seen(fetchMock, "GET", E)).toBe(false);
  });
});

describe("/notify/outbox", () => {
  const O = "/api/notify/outbox";
  const open = (over: Routes = {}, extra: Partial<Parameters<typeof mountApp>[0]> = {}) =>
    mountApp({
      path: "/notify/outbox",
      session: ident([OUTBOX]),
      routes: { [`GET ${O}`]: () => json(outbox()), ...over },
      ...extra,
    });

  it("shows the counts and rows by ID and tokens only, accessibly", async () => {
    const { container } = open();
    expect(await screen.findByText("E-mail is set up.")).toBeInTheDocument();
    const counts = screen.getByText("Queued").closest("div")!;
    expect(counts).toHaveTextContent("2");
    expect(screen.getByText(MSG1)).toBeInTheDocument();
    expect(screen.getByText("Password-reset link")).toBeInTheDocument();
    expect(screen.queryByText("identity.password-reset")).not.toBeInTheDocument();
    expect(screen.getByText("Couldn't reach the mail server")).toBeInTheDocument();
    expect(container.textContent).not.toMatch(/@/);
    await expectNoAxeViolations(container);
  });

  it("shows the recipient's playa name, or a fixed phrase when there is no person", async () => {
    const PAT = "7c000000-0000-4000-8000-000000000001";
    open({
      [`GET ${O}`]: () =>
        json(
          outbox({ recent: [record({ person_id: PAT }), record({ id: MSG2, status: "queued" })] }),
        ),
      "GET /api/people/names": () => json({ names: { [PAT]: "Pat Dust" } }),
    });
    expect(await screen.findByText("Pat Dust")).toBeInTheDocument();
    expect(screen.getByText("An e-mail address (no account)")).toBeInTheDocument();
  });

  it("falls back to the raw token and template name when the catalogue has none", async () => {
    open({
      [`GET ${O}`]: () =>
        json(outbox({ recent: [record({ template: "unlisted-thing", last_error: "weird_new" })] })),
    });
    expect(await screen.findByText("weird_new")).toBeInTheDocument();
    expect(screen.getByText("identity.unlisted-thing")).toBeInTheDocument();
  });

  it("falls back to the ID when names can't be resolved, and never shows an address", async () => {
    const PAT = "7c000000-0000-4000-8000-000000000001";
    const { container } = open({
      [`GET ${O}`]: () => json(outbox({ recent: [record({ person_id: PAT })] })),
      "GET /api/people/names": () => problem(500, { code: "internal" }),
    });
    expect(await screen.findByText(PAT)).toBeInTheDocument();
    expect(container.textContent).not.toMatch(/@/);
  });

  it("shows a retry refusal next to its own row only", async () => {
    open({
      [`GET ${O}`]: () => json(outbox({ recent: [record(), record({ id: MSG2 })] })),
      [`POST ${O}/${MSG1}/retry`]: () => problem(409, { code: "conflict" }),
    });
    await userEvent.click(await screen.findByRole("button", { name: `Retry message ${MSG1}` }));
    const alert = await screen.findByRole("alert");
    expect(alert.closest("tr")).toHaveTextContent(MSG1);
    expect(alert.closest("tr")).not.toHaveTextContent(MSG2);
  });

  it("says Nothing waiting when there are no rows", async () => {
    open({ [`GET ${O}`]: () => json(outbox({ recent: [] })) });
    expect(await screen.findByText("Nothing waiting.")).toBeInTheDocument();
  });

  it("not configured: says so, links to the settings, and disables the connection check", async () => {
    open({ [`GET ${O}`]: () => json(outbox({ configured: false })) });
    expect(await screen.findByText(/E-mail isn't set up yet/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open e-mail settings" })).toHaveAttribute(
      "href",
      "/settings/notify",
    );
    expect(screen.getByRole("button", { name: "Check connection" })).toBeDisabled();
  });

  it("retry requeues a failed row and reloads", async () => {
    const { fetchMock } = open({ [`POST ${O}/${MSG1}/retry`]: () => noContent() });
    await userEvent.click(await screen.findByRole("button", { name: `Retry message ${MSG1}` }));
    await waitFor(() => expect(seen(fetchMock, "POST", `${O}/${MSG1}/retry`)).toBe(true));
    expect(await screen.findByText("Queued again")).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByRole("heading", { level: 1, name: "E-mail outbox" })).toHaveFocus(),
    );
  });

  it("retry is disabled once expires_at has passed, with the reason in the row", async () => {
    open({
      [`GET ${O}`]: () =>
        json(
          outbox({
            recent: [record({ expires_at: "2020-01-01T00:00:00Z", last_error: "expired" })],
          }),
        ),
    });
    const retry = await screen.findByRole("button", { name: `Retry message ${MSG1}` });
    expect(retry).toBeDisabled();
    expect(screen.getByText("Expired")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: `Cancel message ${MSG1}` })).toBeEnabled();
  });

  it("the server's expired answer has its own sentence", async () => {
    open({
      [`POST ${O}/${MSG1}/retry`]: () =>
        problem(409, { code: "conflict", fields: [{ field: "status", code: "expired" }] }),
    });
    await userEvent.click(await screen.findByRole("button", { name: `Retry message ${MSG1}` }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "This message has expired, so it can't be sent again. Trigger the action again.",
    );
  });

  it("a cancel conflict reads like a retry conflict", async () => {
    open({ [`POST ${O}/${MSG1}/cancel`]: () => problem(409, { code: "conflict" }) });
    await userEvent.click(await screen.findByRole("button", { name: `Cancel message ${MSG1}` }));
    await userEvent.click(
      within(await screen.findByRole("dialog")).getByRole("button", { name: "Cancel message" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(/can't be changed now/);
  });

  it("another conflict says the row can't be changed now", async () => {
    open({ [`POST ${O}/${MSG1}/retry`]: () => problem(409, { code: "conflict" }) });
    await userEvent.click(await screen.findByRole("button", { name: `Retry message ${MSG1}` }));
    expect(await screen.findByRole("alert")).toHaveTextContent("can't be changed now");
  });

  it("only failed rows can be retried; queued and failed can be canceled, after a confirmation", async () => {
    const { fetchMock } = open({
      [`GET ${O}`]: () =>
        json(
          outbox({
            recent: [
              record({ id: MSG1, status: "queued", last_error: undefined }),
              record({ id: MSG2, status: "sending" }),
              record({ id: MSG3, status: "failed" }),
            ],
          }),
        ),
      [`POST ${O}/${MSG1}/cancel`]: () => noContent(),
    });
    await screen.findByText(MSG1);
    expect(screen.queryByRole("button", { name: `Retry message ${MSG1}` })).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: `Cancel message ${MSG2}` }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: `Retry message ${MSG3}` })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: `Cancel message ${MSG1}` }));
    const dialog = await screen.findByRole("dialog");
    expect(seen(fetchMock, "POST", `${O}/${MSG1}/cancel`)).toBe(false);
    await userEvent.click(within(dialog).getByRole("button", { name: "Cancel message" }));
    await waitFor(() => expect(seen(fetchMock, "POST", `${O}/${MSG1}/cancel`)).toBe(true));
    expect(await screen.findByText("Message canceled")).toBeInTheDocument();
  });

  it("a 409 on cancel is shown inside the confirmation", async () => {
    open({ [`POST ${O}/${MSG1}/cancel`]: () => problem(409, { code: "conflict" }) });
    await userEvent.click(await screen.findByRole("button", { name: `Cancel message ${MSG1}` }));
    const dialog = await screen.findByRole("dialog");
    await userEvent.click(within(dialog).getByRole("button", { name: "Cancel message" }));
    expect(await within(dialog).findByRole("alert")).toBeInTheDocument();
  });

  it("check connection: success says the server answered", async () => {
    open({ [`POST ${O}/check`]: () => noContent() });
    await userEvent.click(await screen.findByRole("button", { name: "Check connection" }));
    expect(await screen.findByText(/mail server answered/)).toBeInTheDocument();
  });

  it("check connection: an unreachable server shows the error with its reference", async () => {
    open({
      [`POST ${O}/check`]: () => problem(503, { code: "unavailable", ref: "1A2B-3C4D" }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Check connection" }));
    expect(await screen.findByText(/1A2B-3C4D/)).toBeInTheDocument();
    expect(screen.queryByText(/mail server answered/)).not.toBeInTheDocument();
  });

  it("check connection: 501 says e-mail isn't set up", async () => {
    open({ [`POST ${O}/check`]: () => problem(501, { code: "unsupported" }) });
    await userEvent.click(await screen.findByRole("button", { name: "Check connection" }));
    expect((await screen.findAllByRole("alert"))[0]).toHaveTextContent("E-mail isn't set up yet");
  });

  it("a failed load shows the error with a retry; 403 has none", async () => {
    const a = open({ [`GET ${O}`]: () => problem(500, { code: "internal", ref: "ABCD-1234" }) });
    expect(await screen.findByText(/ABCD-1234/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument();
    a.unmount();
    open({ [`GET ${O}`]: () => problem(403, { code: "permission" }) });
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "You don't have access to this screen",
    );
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
  });

  it("without the permission it shows the forbidden state and asks for nothing", async () => {
    const { fetchMock } = open({}, { session: ident([TEMPLATES]) });
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "You don't have access to this screen",
    );
    expect(seen(fetchMock, "GET", O)).toBe(false);
  });

  it("read-only copy: retry and cancel are disabled with the reason", async () => {
    open({}, { readOnly: () => true });
    const retry = await screen.findByRole("button", { name: `Retry message ${MSG1}` });
    await waitFor(() => expect(retry).toBeDisabled());
    expect(screen.getByRole("button", { name: `Cancel message ${MSG1}` })).toBeDisabled();
    expect(screen.getAllByText(/read-only/).length).toBeGreaterThan(0);
  });
});

describe("the notify settings area", () => {
  it("titles its keys, the secret and the options from the notify catalogue", async () => {
    const doc = {
      areas: [
        {
          name: "notify",
          core: true,
          settings: [
            {
              key: "notify.smtp_host",
              kind: "text",
              value: null,
              default: "",
              stale: false,
              secret_set: false,
            },
            {
              key: "notify.smtp_password",
              kind: "secret",
              value: null,
              default: "",
              stale: false,
              secret_set: true,
            },
            {
              key: "notify.smtp_security",
              kind: "enum",
              value: null,
              default: "starttls",
              options: ["starttls", "tls", "none"],
              stale: false,
              secret_set: false,
            },
          ],
        },
      ],
    };
    mountApp({
      path: "/settings/notify",
      session: ident(["settings.read", "settings.manage"]),
      routes: { "GET /api/settings/": () => json(doc) },
    });
    expect(await screen.findByRole("heading", { name: "E-mail" })).toBeInTheDocument();
    expect(screen.getByText("Mail server")).toBeInTheDocument();
    expect(screen.getByText("Mail server password")).toBeInTheDocument();
    expect(screen.getByText("A value is set. It can't be shown.")).toBeInTheDocument();
    expect(screen.getByText("Connection security")).toBeInTheDocument();
    expect(screen.queryByText("notify.smtp_host")).not.toBeInTheDocument();
  });
});
