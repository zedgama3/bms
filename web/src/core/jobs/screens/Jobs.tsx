import { useQueryClient } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { useState } from "react";
import { api, ApiError, useAction, useReadOnly, useSession } from "@/api";
import type { components } from "@/api";
import { isMessageKey, useFormat, useT } from "@/i18n";
import { Banner, Button, EmptyState, ErrorState, Loading, Table, useToast } from "@/ui";
import type { Column } from "@/ui";
import { ConfirmDialog } from "../ConfirmDialog";
import { focusSoon } from "../focus";
import { byId, holdsSite, MAX_RECENT, secondsBetween, spanOf, splitName } from "../helpers";
import { JOBS_KEY, useJobsStatus } from "../queries";
import { ScreenError } from "../ScreenError";

type Rec = components["schemas"]["JobsRecord"];
type Registered = components["schemas"]["JobsRegistered"];
type Section = "failed" | "running" | "retrying" | "waiting";

const cardId = (name: string) => `job-${name}`;

/** `/jobs` (`jobs.read`): what failed, what runs, what waits, and every registered job (jobs.md
 *  J-W1 W1.6). Run now, Retry and Cancel need `jobs.manage` and are hidden on a read-only server.
 *  Only titles are shown: never an ID, a key or an argument (the API sends no arguments). */
export function Jobs() {
  const t = useT();
  const format = useFormat();
  const session = useSession();
  const toast = useToast();
  const client = useQueryClient();
  const { readOnly: headerReadOnly } = useReadOnly();
  const allowed = holdsSite(session, "jobs.read");
  const q = useJobsStatus(allowed);
  const run = useAction();
  const [runName, setRunName] = useState<string | null>(null);
  const row = useAction();
  const [rowId, setRowId] = useState<string | null>(null);
  const cancel = useAction();
  const [canceling, setCanceling] = useState<Rec | null>(null);

  if (!allowed) {
    return (
      <div className="flex flex-col gap-4">
        <h1 className="text-2xl font-bold">{t("core.jobs.title")}</h1>
        <ScreenError />
      </div>
    );
  }

  const error = q.error instanceof ApiError ? q.error : null;
  const data = q.data;
  const readOnly = (data?.read_only ?? false) || headerReadOnly;
  const manage = holdsSite(session, "jobs.manage") && !readOnly;
  const refresh = () => client.invalidateQueries({ queryKey: JOBS_KEY });

  const open = (name: string) =>
    [...(data?.running ?? []), ...(data?.retrying ?? []), ...(data?.waiting ?? [])].some(
      (r) => r.name === name,
    ) ||
    (data?.registered ?? []).some(
      (r) => r.name === name && (r.last?.status === "queued" || r.last?.status === "running"),
    );

  const runNow = (name: string) => {
    setRunName(name);
    void run
      .run(async () => {
        await api.POST("/api/jobs/registered/{name}/run", { params: { path: { name } } });
        toast.show(t("core.jobs.run_started"));
      })
      .then((ok) => {
        void refresh();
        if (ok) focusSoon(byId(cardId(name))); // the button is disabled now
      });
  };

  const retry = (r: Rec) => {
    setRowId(r.id);
    void row
      .run(async () => {
        await api.POST("/api/jobs/queue/{job}/retry", { params: { path: { job: r.id } } });
        toast.show(t("core.jobs.retried"));
      })
      .then((ok) => {
        void refresh();
        if (ok) focusSoon("#jobs-failed"); // the row changes and the pressed button is gone
      });
  };

  const doCancel = () => {
    const r = canceling;
    if (r === null) return;
    void cancel
      .run(async () => {
        await api.POST("/api/jobs/queue/{job}/cancel", { params: { path: { job: r.id } } });
        toast.show(t("core.jobs.canceled"));
      })
      .then((ok) => {
        void refresh();
        if (ok) {
          setCanceling(null);
          // the row is gone with the dialog: focus its own section's heading
          focusSoon(data?.retrying.some((x) => x.id === r.id) ? "#jobs-retrying" : "#jobs-waiting");
        }
      });
  };

  /** The sentence for a refused action; undefined leaves the shell's sentence for its code. */
  const actionMessage = (e: ApiError, what: "run" | "queue"): string | undefined => {
    const code = e.fields.find((f) => f.field === "name" || f.field === "status")?.code ?? e.code;
    if (what === "run") {
      if (e.code === "conflict" || code === "running") return t("core.jobs.run_conflict");
      if (code === "on_demand") return t("core.jobs.run_on_demand");
      if (e.code === "not_found") return t("core.jobs.run_unknown");
      return undefined;
    }
    if (e.code === "conflict") return t("core.jobs.not_open");
    if (e.code === "not_found") return t("core.jobs.gone");
    return undefined;
  };

  /** The catalogue's title for a job or its owner; none when no catalogue has one (W1.4). */
  const look = (...keys: string[]): string | null => {
    for (const k of keys) if (isMessageKey(k)) return t(k);
    return null;
  };
  const ownerTitle = (owner: string) =>
    look(`module.${owner}.title`, `core.${owner}.title`) ?? owner;
  /** A job's title, or its raw name as the fallback (a missing key never hides a job). */
  const titleText = (name: string): string => {
    const { owner, ident } = splitName(name);
    return look(`module.${owner}.job.${ident}.title`, `core.${owner}.job.${ident}.title`) ?? name;
  };
  const hasTitle = (name: string) => titleText(name) !== name;
  const jobTitle = (name: string): ReactNode =>
    hasTitle(name) ? titleText(name) : <code translate="no">{name}</code>;
  /** True when a run was by a server other than the one that answered (F67). */
  const elsewhere = (instance: string | undefined) =>
    instance !== undefined && instance !== "" && instance !== data?.instance;
  const anotherServer = (instance: string) => (
    <span className="block text-sm font-normal [overflow-wrap:anywhere]">
      {t("core.jobs.registered.instance", { instance })}
    </span>
  );

  const statusText = (s: Rec["status"]) => t(`core.jobs.status.${s}`);
  /** A known token in words; an unknown one as it came. */
  const errorText = (token: string): ReactNode => {
    const key = `core.jobs.error.${token}`;
    return isMessageKey(key) ? t(key) : <code translate="no">{token}</code>;
  };
  const span = (seconds: number) => spanOf(seconds);

  const jobColumn: Column<Rec> = {
    header: t("core.jobs.col.name"),
    rowHeader: true,
    cell: (r) => (
      <>
        {jobTitle(r.name)}
        {elsewhere(r.instance) ? anotherServer(r.instance ?? "") : null}
      </>
    ),
  };
  const attempts: Column<Rec> = {
    header: t("core.jobs.col.attempts"),
    cell: (r) => format.number(r.attempts),
  };
  const problem = (header: string): Column<Rec> => ({
    header,
    cell: (r) => (r.last_error ? errorText(r.last_error) : t("core.jobs.none")),
  });
  const actions = (cell: (r: Rec) => ReactNode): Column<Rec>[] =>
    manage ? [{ header: t("core.jobs.col.actions"), cell }] : [];

  const retryCell = (r: Rec) => (
    <Button
      variant="secondary"
      busy={row.busy && rowId === r.id}
      disabled={row.busy && rowId !== r.id}
      aria-label={t("core.jobs.retry_label", {
        title: titleText(r.name),
        when: format.dateTime(r.finished_at ?? r.run_at),
      })}
      onClick={() => retry(r)}
    >
      {t("core.jobs.retry")}
    </Button>
  );
  const cancelCell = (r: Rec) => (
    <Button
      variant="danger"
      aria-label={t("core.jobs.cancel_label", {
        title: titleText(r.name),
        when: format.dateTime(r.run_at),
      })}
      onClick={() => {
        cancel.clear();
        setCanceling(r);
      }}
    >
      {t("core.jobs.cancel")}
    </Button>
  );

  const counts = data?.counts ?? {};
  const total: Record<Section, number | null> = {
    failed: counts["failed"] ?? 0,
    running: counts["running"] ?? 0,
    retrying: null,
    waiting: counts["queued"] ?? 0,
  };

  /** One section: its heading, its table (or empty state) and the cap line. */
  const section = (
    id: Section,
    rows: readonly Rec[],
    columns: Column<Rec>[],
    error: ReactNode = null,
  ) => (
    <section aria-labelledby={`jobs-${id}`} className="flex flex-col gap-3">
      <h2
        id={`jobs-${id}`}
        tabIndex={-1}
        className="text-xl font-bold focus:[outline:2px_solid_var(--focus)] focus:outline-offset-2"
      >
        {t(`core.jobs.${id}.title`)}
      </h2>
      {error}
      {rows.length === 0 ? (
        <EmptyState>{t(`core.jobs.${id}.empty`)}</EmptyState>
      ) : (
        <Table
          caption={t(`core.jobs.${id}.caption`)}
          columns={columns}
          rows={rows}
          rowKey={(r) => r.id}
        />
      )}
      {rows.length >= MAX_RECENT ? (
        <p className="text-sm text-muted">
          {t(`core.jobs.${id}.capped`, {
            n: format.number(rows.length),
            total: format.number(total[id] ?? rows.length),
          })}
        </p>
      ) : null}
    </section>
  );

  const jump = (id: Section) => (e: { preventDefault: () => void }) => {
    e.preventDefault();
    focusSoon(`#jobs-${id}`);
  };
  const summary = (id: Section, count: number, key: "failed" | "running" | "waiting") => (
    <li key={id}>
      <a
        href={`#jobs-${id}`}
        onClick={jump(id)}
        className="inline-flex min-h-11 items-center whitespace-nowrap rounded-md border border-border px-3 font-semibold underline"
      >
        {t(`core.jobs.summary.${key}`, { count: format.number(count) })}
      </a>
    </li>
  );

  const owners = [...new Set((data?.registered ?? []).map((r) => r.owner))]
    .map((o) => ({ owner: o, title: ownerTitle(o) }))
    .sort((a, b) => a.title.localeCompare(b.title));
  /** Scheduled names first, then on-demand, each by title. */
  const ordered = (owner: string) =>
    (data?.registered ?? [])
      .filter((r) => r.owner === owner)
      .sort(
        (a, b) =>
          Number(a.every_seconds === 0) - Number(b.every_seconds === 0) ||
          titleText(a.name).localeCompare(titleText(b.name)),
      );

  const card = (r: Registered) => {
    const waiting = open(r.name);
    const onDemand = r.every_seconds === 0;
    const took = secondsBetween(r.last?.started_at, r.last?.finished_at);
    const runError = run.error && runName === r.name ? run.error : null;
    const waitId = `${cardId(r.name)}-waiting`;
    return (
      <li key={r.name} className="flex flex-col gap-2 rounded-md border border-border p-4">
        <h4 id={cardId(r.name)} tabIndex={-1} className="font-bold focus:outline-none">
          {jobTitle(r.name)}
        </h4>
        <p>
          {onDemand
            ? t("core.jobs.registered.on_demand")
            : t("core.jobs.registered.every", { ...span(r.every_seconds) })}
        </p>
        {r.last ? (
          <>
            <p>
              {t(took !== null ? "core.jobs.registered.last_took" : "core.jobs.registered.last", {
                status: statusText(r.last.status),
                when: format.dateTime(r.last.finished_at ?? r.last.started_at ?? r.last.run_at),
                ...span(took ?? 0),
              })}
            </p>
            {elsewhere(r.last.instance) ? (
              <p className="text-sm">
                {t("core.jobs.registered.instance", { instance: r.last.instance ?? "" })}
              </p>
            ) : null}
            {r.last.last_error ? (
              <p>
                {t("core.jobs.registered.error")} {errorText(r.last.last_error)}
              </p>
            ) : null}
          </>
        ) : (
          <p>{t("core.jobs.registered.never")}</p>
        )}
        {r.next_due ? (
          <p>{t("core.jobs.registered.next", { when: format.dateTime(r.next_due) })}</p>
        ) : null}
        {manage && !onDemand ? (
          <div className="flex flex-col items-start gap-2">
            <Button
              variant="secondary"
              busy={run.busy && runName === r.name}
              disabled={waiting || (run.busy && runName !== r.name)}
              {...(waiting ? { "aria-describedby": waitId } : {})}
              aria-label={t("core.jobs.run_label", { title: titleText(r.name) })}
              onClick={() => runNow(r.name)}
            >
              {t("core.jobs.run")}
            </Button>
            {waiting ? (
              <p id={waitId} className="text-sm font-semibold">
                {t("core.jobs.run_waiting")}
              </p>
            ) : null}
            {runError ? (
              <ErrorState error={runError} {...msg(actionMessage(runError, "run"))} />
            ) : null}
          </div>
        ) : null}
      </li>
    );
  };

  const rowError = row.error ? (
    <ErrorState error={row.error} {...msg(actionMessage(row.error, "queue"))} />
  ) : null;

  return (
    <div className="flex flex-col gap-6" aria-busy={q.isPending || undefined}>
      <h1 id="jobs-title" tabIndex={-1} className="text-2xl font-bold focus:outline-none">
        {t("core.jobs.title")}
      </h1>
      <p>{t("core.jobs.intro")}</p>
      {q.isPending ? <Loading /> : null}
      {error ? <ScreenError error={error} onRetry={() => void q.refetch()} /> : null}
      {data ? (
        <>
          {readOnly ? <Banner tone="warn">{t("core.jobs.readonly_banner")}</Banner> : null}
          <div>
            <Button variant="secondary" onClick={() => void q.refetch()}>
              {t("core.jobs.refresh")}
            </Button>
          </div>
          <nav aria-label={t("core.jobs.summary.label")}>
            <ul className="flex flex-wrap gap-x-3 gap-y-2">
              {summary("failed", total.failed ?? 0, "failed")}
              {summary("running", total.running ?? 0, "running")}
              {summary("waiting", total.waiting ?? 0, "waiting")}
            </ul>
          </nav>
          {section(
            "failed",
            data.failed,
            [
              jobColumn,
              {
                header: t("core.jobs.col.failed"),
                cell: (r) => format.dateTime(r.finished_at ?? r.run_at),
              },
              attempts,
              problem(t("core.jobs.col.error")),
              ...actions(retryCell),
            ],
            rowError,
          )}
          {section("running", data.running, [
            jobColumn,
            {
              header: t("core.jobs.col.started"),
              cell: (r) => format.dateTime(r.started_at ?? r.run_at),
            },
            { header: t("core.jobs.col.attempt"), cell: (r) => format.number(r.attempts) },
          ])}
          {section("retrying", data.retrying, [
            jobColumn,
            { header: t("core.jobs.col.due"), cell: (r) => format.dateTime(r.run_at) },
            attempts,
            problem(t("core.jobs.col.last_error")),
            ...actions(cancelCell),
          ])}
          {section("waiting", data.waiting, [
            jobColumn,
            { header: t("core.jobs.col.due"), cell: (r) => format.dateTime(r.run_at) },
            ...actions(cancelCell),
          ])}
          <section aria-labelledby="jobs-registered" className="flex flex-col gap-4">
            <h2 id="jobs-registered" className="text-xl font-bold">
              {t("core.jobs.registered.title")}
            </h2>
            {data.registered.length === 0 ? (
              <EmptyState>{t("core.jobs.registered.empty")}</EmptyState>
            ) : (
              owners.map(({ owner, title }) => (
                <section
                  key={owner}
                  aria-labelledby={`jobs-owner-${owner}`}
                  className="flex flex-col gap-3"
                >
                  <h3 id={`jobs-owner-${owner}`} className="text-lg font-bold">
                    {title === owner ? <span translate="no">{title}</span> : title}
                  </h3>
                  <ul className="grid gap-3 sm:grid-cols-2">{ordered(owner).map(card)}</ul>
                </section>
              ))
            )}
          </section>
        </>
      ) : null}
      <ConfirmDialog
        open={canceling !== null}
        title={t("core.jobs.cancel_title")}
        body={t("core.jobs.cancel_body")}
        confirm={t("core.jobs.cancel")}
        busy={cancel.busy}
        error={cancel.error}
        {...msg(cancel.error ? actionMessage(cancel.error, "queue") : undefined)}
        onConfirm={doCancel}
        onClose={() => setCanceling(null)}
      />
    </div>
  );
}

function msg(m: string | undefined): { message?: string } {
  return m === undefined ? {} : { message: m };
}
