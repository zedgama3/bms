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

const status = (over: Record<string, unknown> = {}) => ({
  counts: { queued: 1, failed: 1 },
  read_only: false,
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
    {
      name: "weird.thing",
      owner: "weird",
      every_seconds: 90,
      timeout_seconds: 60,
      last: rec({ name: "weird.thing", last_error: "odd_new_token" }),
    },
  ],
  recent: [
    rec(),
    rec({
      id: WAITING,
      name: "jobs.purge",
      status: "queued",
      attempts: 0,
      last_error: undefined,
      key: "admin:x",
    }),
  ],
  ...over,
});

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

  it("shows registered work, recent rows and translated tokens, accessibly", async () => {
    const { container } = open();
    expect(await screen.findByRole("heading", { level: 1, name: "Background work" })).toBeVisible();
    expect(await screen.findByText("Runs every 1 hour")).toBeInTheDocument();
    expect(screen.getByText("Runs every 90 seconds")).toBeInTheDocument();
    expect(screen.getByText("Runs on demand")).toBeInTheDocument();
    expect(screen.getAllByText("The job kept failing and was given up on").length).toBeGreaterThan(
      0,
    );
    expect(screen.getByText("odd_new_token").tagName).toBe("CODE");
    expect(screen.getByText("odd_new_token")).toHaveAttribute("translate", "no");
    expect(screen.getAllByText(/Took 4 seconds/).length).toBeGreaterThan(0);
    await expectNoAxeViolations(container);
  });

  it("starts a scheduled job and refreshes; on-demand work has no button", async () => {
    let posted = 0;
    open({
      [`POST ${J}/registered/weird.thing/run`]: () => {
        posted++;
        return json({ id: WAITING }, { status: 202 });
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: "Run now: weird.thing" }));
    await waitFor(() => expect(posted).toBe(1));
    expect(screen.queryByRole("button", { name: /signals.redeliver/ })).not.toBeInTheDocument();
  });

  it("disables Run now with the reason while a run waits", async () => {
    open();
    const button = await screen.findByRole("button", { name: "Run now: jobs.purge" });
    expect(button).toBeDisabled();
    expect(button).toHaveAccessibleDescription("Already waiting or running.");
  });

  it("shows a refused run next to its own card", async () => {
    open({ [`POST ${J}/registered/weird.thing/run`]: () => problem(409, { code: "conflict" }) });
    await userEvent.click(await screen.findByRole("button", { name: "Run now: weird.thing" }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("That work is already waiting or running.");
    expect(alert.closest("li")).toHaveTextContent("weird.thing");
  });

  it("retries a failed row", async () => {
    let posted = 0;
    open({
      [`POST ${J}/queue/${FAILED}/retry`]: () => {
        posted++;
        return json({});
      },
    });
    const b = await screen.findByRole("button", { name: /^Retry audit\.purge/ });
    await userEvent.click(b);
    await waitFor(() => expect(posted).toBe(1));
    expect(await screen.findByText("Queued again")).toBeInTheDocument();
  });

  it("keeps a retry refusal on screen after the row is gone", async () => {
    let gets = 0;
    open({
      [`GET ${J}/`]: () => {
        gets++;
        return json(gets === 1 ? status() : status({ recent: [] }));
      },
      [`POST ${J}/queue/${FAILED}/retry`]: () => problem(409, { code: "conflict" }),
    });
    await userEvent.click(await screen.findByRole("button", { name: /^Retry audit\.purge/ }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("can't be changed this way any more");
    await waitFor(() => expect(gets).toBeGreaterThan(1));
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: /^Retry audit\.purge/ })).not.toBeInTheDocument(),
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
    await userEvent.click(await screen.findByRole("button", { name: /^Cancel jobs\.purge/ }));
    const dialog = await screen.findByRole("dialog");
    expect(posted).toBe(0);
    await userEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(posted).toBe(1));
    expect(await screen.findByText("Job canceled")).toBeInTheDocument();
  });

  it("keeps a cancel refusal in the dialog", async () => {
    open({ [`POST ${J}/queue/${WAITING}/cancel`]: () => problem(409, { code: "conflict" }) });
    await userEvent.click(await screen.findByRole("button", { name: /^Cancel jobs\.purge/ }));
    const dialog = await screen.findByRole("dialog");
    await userEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("can't be changed");
  });

  it("hides the actions without jobs.manage", async () => {
    open({}, [READ]);
    await screen.findByText("Runs on demand");
    expect(screen.queryByRole("button", { name: /^Run now/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Retry/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Cancel/ })).not.toBeInTheDocument();
  });

  it("read-only copy: says so and hides the actions", async () => {
    open({ [`GET ${J}/`]: () => json(status({ read_only: true })) });
    expect(await screen.findByText(/runs no background work/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Run now/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Retry/ })).not.toBeInTheDocument();
  });

  it("says so when nothing is registered or waiting", async () => {
    open({ [`GET ${J}/`]: () => json(status({ registered: [], recent: [] })) });
    expect(await screen.findByText("Nothing is registered.")).toBeInTheDocument();
    expect(screen.getByText("Nothing waiting or failed.")).toBeInTheDocument();
  });

  it("shows the forbidden state without jobs.read, asking nothing", async () => {
    open({}, []);
    expect(await screen.findByText("You don't have access to this screen.")).toBeInTheDocument();
  });

  it("shows the forbidden sentence for a 403 on the read", async () => {
    open({ [`GET ${J}/`]: () => problem(403, { code: "permission" }) });
    expect(await screen.findByText("You don't have access to this screen.")).toBeInTheDocument();
  });

  it("shows an error with a retry when the read fails", async () => {
    let n = 0;
    open({
      [`GET ${J}/`]: () =>
        n++ === 0 ? problem(500, { code: "internal", ref: "AAAA-0001" }) : json(status()),
    });
    const retry = await screen.findByRole("button", { name: /try again|retry/i });
    await userEvent.click(retry);
    expect(await screen.findByText("Runs on demand")).toBeInTheDocument();
  });

  it("has a navigation entry for holders of jobs.read", async () => {
    open({}, [READ], { path: "/" });
    expect(await screen.findAllByRole("link", { name: "Background work" })).not.toHaveLength(0);
  });
});
