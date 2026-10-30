import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { expectNoAxeViolations } from "@/test/axe";
import { mountApp } from "@/test/app";
import { json, problem } from "@/test/fakeFetch";

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
const READ = "jobs.read";
const MANAGE = "jobs.manage";
const J = "/api/jobs";
const FAILED = "5b1f0000-0000-4000-8000-000000000001";
const WAITING = "5b1f0000-0000-4000-8000-000000000002";
const RETRYING = "5b1f0000-0000-4000-8000-000000000003";
const RUNNING = "5b1f0000-0000-4000-8000-000000000004";

const rec = (over: Record<string, unknown> = {}) => ({
  id: FAILED,
  name: "audit.purge",
  status: "failed",
  attempts: 3,
  run_at: "2026-10-19T10:00:00Z",
  started_at: "2026-10-19T10:00:01Z",
  finished_at: "2026-10-19T10:00:05Z",
  last_error: "gave_up",
  instance: "web-1",
  ...over,
});

const waitingRow = (over: Record<string, unknown> = {}) =>
  rec({
    id: WAITING,
    name: "jobs.purge",
    status: "queued",
    attempts: 0,
    last_error: undefined,
    finished_at: undefined,
    started_at: undefined,
    key: "5b1f0000-aaaa-4000-8000-0000000000aa",
    ...over,
  });

const status = (over: Record<string, unknown> = {}) => ({
  counts: { queued: 2, failed: 1, running: 1 },
  read_only: false,
  instance: "web-1",
  registered: [
    {
      name: "jobs.purge",
      owner: "jobs",
      every_seconds: 3600,
      timeout_seconds: 600,
      next_due: "2026-10-20T12:00:00Z",
      last: rec({
        id: "5b1f0000-0000-4000-8000-000000000009",
        name: "jobs.purge",
        status: "done",
        last_error: undefined,
      }),
    },
    {
      name: "signals.redeliver",
      owner: "signals",
      every_seconds: 0,
      timeout_seconds: 60,
    },
    { name: "files.reap", owner: "files", every_seconds: 0, timeout_seconds: 60 },
    {
      name: "weird.thing",
      owner: "weird",
      every_seconds: 90,
      timeout_seconds: 60,
      last: rec({ name: "weird.thing", last_error: "odd_new_token" }),
    },
  ],
  failed: [rec()],
  running: [
    rec({
      id: RUNNING,
      name: "files.sweep-orphans",
      status: "running",
      attempts: 1,
      last_error: undefined,
      finished_at: undefined,
    }),
  ],
  retrying: [
    rec({
      id: RETRYING,
      name: "files.reap",
      status: "queued",
      attempts: 2,
      last_error: "unavailable",
      finished_at: undefined,
    }),
  ],
  waiting: [waitingRow()],
  ...over,
});

const lists = (over: Record<string, unknown> = {}) =>
  status({ failed: [], running: [], retrying: [], waiting: [], counts: {}, ...over });

describe("/jobs", () => {
  const open = (
    routes: Parameters<typeof mountApp>[0] extends infer M
      ? NonNullable<M extends { routes?: infer R } ? R : never>
      : never = {},
    perms = [READ, MANAGE],
    extra: Partial<Parameters<typeof mountApp>[0]> = {},
  ) =>
    mountApp({
      path: "/jobs",
      session: ident(perms),
      routes: { [`GET ${J}/`]: () => json(status()), ...routes },
      ...extra,
    });

  const section = (name: string) => {
    const h = screen.getByRole("heading", { level: 2, name });
    return within(h.closest("section")!);
  };

  it("shows titles, never a raw name, an ID or a key, and is accessible", async () => {
    const { container } = open();
    expect(await screen.findByRole("heading", { level: 1, name: "Background work" })).toBeVisible();
    expect(await screen.findByText("Runs every 1 hour")).toBeInTheDocument();
    expect(screen.getByText("Runs every 90 seconds")).toBeInTheDocument();
    expect(screen.getAllByText("Runs when something asks for it")).toHaveLength(2);
    // Titles from the catalogues, and the owners' titles above the cards.
    expect(screen.getAllByText("Delete finished job records").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Delete audit entries past their retention").length).toBeGreaterThan(
      0,
    );
    expect(
      screen.getByRole("heading", { level: 3, name: "Messages between modules" }),
    ).toBeVisible();
    expect(screen.getByRole("heading", { level: 3, name: "Files" })).toBeVisible();
    expect(
      screen.getByRole("heading", { level: 4, name: "Deliver a message between modules again" }),
    ).toBeVisible();
    // No ID, key or raw name for a job whose title exists.
    expect(screen.queryByText(/5b1f0000/)).toBeNull();
    expect(container.textContent).not.toMatch(/5b1f0000/);
    for (const raw of ["audit.purge", "jobs.purge", "signals.redeliver", "files.reap"])
      expect(screen.queryByText(raw)).toBeNull();
    expect(screen.queryByText("Key")).toBeNull();
    expect(screen.getAllByText("The job kept failing and was given up on").length).toBeGreaterThan(
      0,
    );
    expect(screen.getAllByText(/Took 4 seconds/).length).toBeGreaterThan(0);
    await expectNoAxeViolations(container);
  });

  it("shows a raw name in code when the catalogue has no title", async () => {
    open();
    await screen.findByText("Runs every 90 seconds");
    const name = screen.getByRole("heading", { level: 4, name: "weird.thing" });
    expect(within(name).getByText("weird.thing").tagName).toBe("CODE");
    expect(within(name).getByText("weird.thing")).toHaveAttribute("translate", "no");
    expect(screen.getByRole("heading", { level: 3, name: "weird" })).toBeVisible();
    expect(screen.getByText("odd_new_token").tagName).toBe("CODE");
  });

  it("lists each state under its own heading with its own columns", async () => {
    open();
    await screen.findByText("Runs every 1 hour");
    const failed = section("Failed");
    expect(failed.getByRole("table", { name: "Failed jobs, newest first" })).toBeVisible();
    for (const h of ["Job", "Failed", "Attempts", "Problem", "Actions"])
      expect(failed.getByRole("columnheader", { name: h })).toBeVisible();

    const running = section("Running now");
    expect(running.getByText("Remove uploads that were never attached")).toBeVisible();
    for (const h of ["Job", "Started", "Attempt"])
      expect(running.getByRole("columnheader", { name: h })).toBeVisible();
    expect(running.queryByRole("button")).toBeNull();

    const retrying = section("Waiting to retry");
    expect(retrying.getByText("Delete the content of deleted files")).toBeVisible();
    expect(retrying.getByText("A service the job needs was unavailable")).toBeVisible();
    for (const h of ["Job", "Due", "Attempts", "Last problem", "Actions"])
      expect(retrying.getByRole("columnheader", { name: h })).toBeVisible();

    // A healthy waiting row has no Status, Attempts or Problem column.
    const waiting = section("Waiting");
    expect(waiting.getByText("Delete finished job records")).toBeVisible();
    const heads = waiting.getAllByRole("columnheader").map((h) => h.textContent);
    expect(heads).toEqual(["Job", "Due", "Actions"]);
    expect(waiting.queryByText("None")).toBeNull();
  });

  it("offers Retry only under Failed and Cancel only under the two waiting sections", async () => {
    open();
    await screen.findByText("Runs every 1 hour");
    expect(section("Failed").getAllByRole("button")).toHaveLength(1);
    expect(
      section("Failed").getByRole("button", {
        name: /^Retry Delete audit entries past their retention, failed /,
      }),
    ).toBeVisible();
    expect(section("Waiting to retry").getAllByRole("button")).toHaveLength(1);
    expect(
      section("Waiting to retry").getByRole("button", {
        name: /^Cancel Delete the content of deleted files, due /,
      }),
    ).toBeVisible();
    expect(
      section("Waiting").getByRole("button", { name: /^Cancel Delete finished job records, due / }),
    ).toBeVisible();
    expect(screen.getAllByRole("button", { name: /^Retry / })).toHaveLength(1);
    expect(screen.getAllByRole("button", { name: /^Cancel / })).toHaveLength(2);
  });

  it("has a counts line with links to the sections", async () => {
    open();
    const nav = await screen.findByRole("navigation", { name: "Sections" });
    const failed = within(nav).getByRole("link", { name: "Failed (1)" });
    expect(failed).toHaveAttribute("href", "#jobs-failed");
    expect(within(nav).getByRole("link", { name: "Running (1)" })).toHaveAttribute(
      "href",
      "#jobs-running",
    );
    await userEvent.click(within(nav).getByRole("link", { name: "Waiting (2)" }));
    await waitFor(() =>
      expect(screen.getByRole("heading", { level: 2, name: "Waiting" })).toHaveFocus(),
    );
  });

  it("shows each empty state", async () => {
    open({ [`GET ${J}/`]: () => json(lists({ registered: [] })) });
    expect(await screen.findByText("Nothing is registered.")).toBeInTheDocument();
    expect(screen.getByText("Nothing has failed.")).toBeInTheDocument();
    expect(screen.getByText("Nothing is running right now.")).toBeInTheDocument();
    expect(screen.getByText("Nothing is waiting to retry.")).toBeInTheDocument();
    expect(screen.getByText("Nothing is waiting.")).toBeInTheDocument();
  });

  it("says how many are shown when a list is full", async () => {
    const many = Array.from({ length: 50 }, (_, i) =>
      waitingRow({ id: `5b1f0000-0000-4000-8000-0000000001${String(i).padStart(2, "0")}` }),
    );
    open({ [`GET ${J}/`]: () => json(status({ waiting: many, counts: { queued: 63 } })) });
    expect(await screen.findByText("The first 50 are shown; 63 are waiting in all.")).toBeVisible();
  });

  it("marks a run by another server and nothing on the home server", async () => {
    open({
      [`GET ${J}/`]: () =>
        json(
          status({
            running: [
              rec({
                id: RUNNING,
                name: "files.sweep-orphans",
                status: "running",
                instance: "web-2",
                last_error: undefined,
              }),
            ],
            registered: [
              {
                name: "jobs.purge",
                owner: "jobs",
                every_seconds: 3600,
                timeout_seconds: 600,
                last: rec({ name: "jobs.purge", status: "done", instance: "web-2" }),
              },
              {
                name: "files.reap",
                owner: "files",
                every_seconds: 3600,
                timeout_seconds: 600,
                last: rec({ name: "files.reap", status: "done", instance: "web-1" }),
              },
            ],
          }),
        ),
    });
    await screen.findAllByText("Runs every 1 hour");
    const elsewhere = screen.getAllByText("Ran on another server (web-2)");
    expect(elsewhere).toHaveLength(2); // the running row and the card
    expect(section("Running now").getByText("Ran on another server (web-2)")).toBeVisible();
    expect(screen.queryByText(/web-1/)).toBeNull();
    expect(screen.queryByText(/On copy/)).toBeNull();
  });

  it("starts a scheduled job and refreshes; on-demand jobs have no button", async () => {
    let posted = 0;
    open({
      [`POST ${J}/registered/weird.thing/run`]: () => {
        posted++;
        return json({ id: WAITING }, { status: 202 });
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: "Run now: weird.thing" }));
    await waitFor(() => expect(posted).toBe(1));
    // The pressed button is disabled now: focus moves to the card.
    await waitFor(() => expect(document.getElementById("job-weird.thing")).toHaveFocus());
    expect(
      screen.queryByRole("button", { name: /Deliver a message between modules again/ }),
    ).not.toBeInTheDocument();
  });

  it("disables Run now with the reason while a row of the job waits", async () => {
    open();
    const button = await screen.findByRole("button", {
      name: "Run now: Delete finished job records",
    });
    expect(button).toBeDisabled();
    expect(button).toHaveAccessibleDescription("Already waiting or running.");
  });

  it("shows a refused run next to its own card", async () => {
    open({ [`POST ${J}/registered/weird.thing/run`]: () => problem(409, { code: "conflict" }) });
    await userEvent.click(await screen.findByRole("button", { name: "Run now: weird.thing" }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("That job is already waiting or running.");
    expect(alert.closest("li")).toHaveTextContent("weird.thing");
  });

  it("shows the error summary with the reference when Run now is refused with 403", async () => {
    open({
      [`POST ${J}/registered/weird.thing/run`]: () =>
        problem(403, { code: "permission", ref: "AAAA-0403" }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Run now: weird.thing" }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("You don't have permission to do that.");
    expect(alert).toHaveTextContent("AAAA-0403");
    expect(alert.closest("li")).toHaveTextContent("weird.thing");
  });

  it("retries a failed row and moves focus to the Failed heading", async () => {
    let posted = 0;
    open({
      [`POST ${J}/queue/${FAILED}/retry`]: () => {
        posted++;
        return json({});
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: /^Retry / }));
    await waitFor(() => expect(posted).toBe(1));
    expect(await screen.findByText("Queued again")).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByRole("heading", { level: 2, name: "Failed" })).toHaveFocus(),
    );
  });

  it("shows the error block with the reference when Retry is refused with 403", async () => {
    open({
      [`POST ${J}/queue/${FAILED}/retry`]: () =>
        problem(403, { code: "permission", ref: "AAAA-0404" }),
    });
    await userEvent.click(await screen.findByRole("button", { name: /^Retry / }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("You don't have permission to do that.");
    expect(alert).toHaveTextContent("AAAA-0404");
    expect(section("Failed").getByRole("alert")).toBe(alert);
  });

  it("keeps a retry refusal on screen after the row is gone", async () => {
    let gets = 0;
    open({
      [`GET ${J}/`]: () => {
        gets++;
        return json(gets === 1 ? status() : status({ failed: [] }));
      },
      [`POST ${J}/queue/${FAILED}/retry`]: () => problem(409, { code: "conflict" }),
    });
    await userEvent.click(await screen.findByRole("button", { name: /^Retry / }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("can't be changed this way any more");
    await waitFor(() => expect(gets).toBeGreaterThan(1));
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: /^Retry / })).not.toBeInTheDocument(),
    );
    expect(screen.getByRole("alert")).toHaveTextContent("can't be changed this way any more");
    expect(screen.getByRole("alert").closest("tr")).toBeNull();
  });

  it("cancels a waiting row after confirming", async () => {
    let posted = 0;
    open({
      [`POST ${J}/queue/${WAITING}/cancel`]: () => {
        posted++;
        return json({});
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: /^Cancel Delete finished/ }));
    const dialog = await screen.findByRole("dialog");
    expect(posted).toBe(0);
    await userEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(posted).toBe(1));
    expect(await screen.findByText("Job canceled")).toBeInTheDocument();
  });

  it("keeps a cancel refusal in the dialog", async () => {
    open({ [`POST ${J}/queue/${WAITING}/cancel`]: () => problem(409, { code: "conflict" }) });
    await userEvent.click(await screen.findByRole("button", { name: /^Cancel Delete finished/ }));
    const dialog = await screen.findByRole("dialog");
    await userEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("can't be changed");
  });

  it("keeps a 403 on Cancel in the dialog", async () => {
    open({
      [`POST ${J}/queue/${WAITING}/cancel`]: () =>
        problem(403, { code: "permission", ref: "AAAA-0405" }),
    });
    await userEvent.click(await screen.findByRole("button", { name: /^Cancel Delete finished/ }));
    const dialog = await screen.findByRole("dialog");
    await userEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    const alert = await within(dialog).findByRole("alert");
    expect(alert).toHaveTextContent("You don't have permission to do that.");
    expect(alert).toHaveTextContent("AAAA-0405");
    expect(screen.getByRole("dialog")).toBeVisible();
  });

  it("hides the actions without jobs.manage", async () => {
    open({}, [READ]);
    await screen.findAllByText("Runs when something asks for it");
    expect(screen.queryByRole("button", { name: /^Run now/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Retry/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Cancel/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("columnheader", { name: "Actions" })).not.toBeInTheDocument();
  });

  it("read-only server: says so and hides the actions", async () => {
    open({ [`GET ${J}/`]: () => json(status({ read_only: true })) });
    expect(await screen.findByText(/runs no background work/)).toBeInTheDocument();
    expect(screen.getByText(/This server is read-only/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Run now/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Retry/ })).not.toBeInTheDocument();
  });

  it("shows the forbidden state without jobs.read, asking nothing", async () => {
    const mock = open({}, []);
    expect(await screen.findByText("You don't have access to this screen.")).toBeInTheDocument();
    expect(screen.getByRole("alert")).toBeVisible();
    expect(screen.queryByText(/reference/i)).toBeNull();
    expect(
      mock.fetchMock.mock.calls.some(([r]) => new URL(r.url).pathname.startsWith("/api/jobs")),
    ).toBe(false);
  });

  it("shows the forbidden sentence and the reference for a 403 on the read", async () => {
    open({ [`GET ${J}/`]: () => problem(403, { code: "permission", ref: "AAAA-0403" }) });
    expect(await screen.findByText("You don't have access to this screen.")).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("AAAA-0403");
    expect(screen.queryByRole("button", { name: /try again|retry/i })).toBeNull();
  });

  it("shows an error with a retry when the read fails", async () => {
    let n = 0;
    open({
      [`GET ${J}/`]: () =>
        n++ === 0 ? problem(500, { code: "internal", ref: "AAAA-0001" }) : json(status()),
    });
    expect(await screen.findByRole("alert")).toHaveTextContent("AAAA-0001");
    await userEvent.click(await screen.findByRole("button", { name: /try again|retry/i }));
    expect((await screen.findAllByText("Runs when something asks for it"))[0]).toBeInTheDocument();
  });

  it("has a navigation entry for holders of jobs.read", async () => {
    open({}, [READ], { path: "/" });
    expect(await screen.findAllByRole("link", { name: "Background work" })).not.toHaveLength(0);
  });
});
