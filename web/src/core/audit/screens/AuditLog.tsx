import { useRef, useState } from "react";
import type { ChangeEvent, FormEvent } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useSearchParams } from "react-router";
import { api, ApiError, searchPeople, useAction, useNames, useSession } from "@/api";
import type { components } from "@/api";
import { isMessageKey, useFormat, useT } from "@/i18n";
import {
  Button,
  Check,
  DateField,
  EmptyState,
  endOfDayInstant,
  ErrorSummary,
  Field,
  Loading,
  useFieldErrors,
  useToast,
  PersonSearch,
} from "@/ui";
import { saveText } from "../download";
import { focusSoon } from "../focus";
import { holds } from "../helpers";
import { ScreenError } from "../ScreenError";

type Record_ = components["schemas"]["AuditRecord"];

const TEXT_FILTERS = ["record_kind", "record_id", "area", "action"] as const;

/** A date-only value as the instant its day starts, in the browser's zone. */
function startOfDay(date: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date);
  if (!m) return "";
  return new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3])).toISOString();
}

/** `/audit`: the audit log, newest first, with filters and keyset paging. Filters are IDs, action
 *  names and dates, so they live in the address (`?subject=<person>` comes from the person
 *  page); the names of people are looked up in one batch and never put in the address. */
export function AuditLog() {
  const t = useT();
  const format = useFormat();
  const toast = useToast();
  const session = useSession();
  const [params, setParams] = useSearchParams();
  const formRef = useRef<HTMLFormElement>(null);
  const exporting = useAction();
  const [dateError, setDateError] = useState(false);
  // The text filters are drafts until Apply: re-rendering never wipes what was typed.
  const fromUrl = (cur: URLSearchParams) => ({
    area: cur.get("area") ?? "",
    action: cur.get("action") ?? "",
    record_kind: cur.get("record_kind") ?? "",
    record_id: cur.get("record_id") ?? "",
    from: cur.get("from") ?? "",
    to: cur.get("to") ?? "",
    superuser: cur.get("superuser") === "true",
  });
  const [draft, setDraft] = useState(() => fromUrl(params));
  const edit = (k: keyof typeof draft) => (e: ChangeEvent<HTMLInputElement>) =>
    setDraft((d) => ({ ...d, [k]: k === "superuser" ? e.target.checked : e.target.value }));

  const canSearch = holds(session, "audit.search");
  const canExport = holds(session, "audit.export");
  const get = (k: string) => params.get(k) ?? "";
  const subject = get("subject");
  const actor = get("actor");
  const from = get("from");
  const to = get("to");
  const superuser = get("superuser") === "true";

  const query = {
    ...(subject ? { subject } : {}),
    ...(actor ? { actor } : {}),
    ...Object.fromEntries(TEXT_FILTERS.filter((k) => get(k)).map((k) => [k, get(k)])),
    ...(from && to ? { from: startOfDay(from), to: endOfDayInstant(to) } : {}),
    ...(superuser ? { superuser: true } : {}),
  };
  const q = useInfiniteQuery({
    queryKey: ["audit", "entries", query],
    enabled: canSearch,
    initialPageParam: "",
    queryFn: ({ signal, pageParam }) =>
      api.GET("/api/audit/entries", {
        signal,
        params: { query: { ...query, ...(pageParam ? { after: pageParam } : {}) } },
      }),
    getNextPageParam: (last) => last.next || undefined,
  });
  const records = q.data?.pages.flatMap((p) => p.records) ?? [];
  const nameOf = useNames(
    [subject, actor, ...records.flatMap((r) => [r.subject ?? "", r.actor.person ?? ""])],
    undefined,
    t("shell.person_search.unnamed"),
  );
  const error = q.error instanceof ApiError ? q.error : null;
  const fieldErrors = useFieldErrors(error?.code === "invalid" ? error : null);

  const setParam = (key: string, value: string) =>
    setParams(
      (cur) => {
        const next = new URLSearchParams(cur);
        if (value) next.set(key, value);
        else next.delete(key);
        return next;
      },
      { replace: true },
    );

  const apply = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const v = (k: "area" | "action" | "record_kind" | "record_id" | "from" | "to") =>
      draft[k].trim();
    const mismatch = (v("from") === "") !== (v("to") === "");
    setDateError(mismatch);
    if (mismatch) return;
    setParams(
      (cur) => {
        const next = new URLSearchParams();
        for (const k of ["subject", "actor"]) if (cur.get(k)) next.set(k, cur.get(k) ?? "");
        for (const k of [...TEXT_FILTERS, "from", "to"] as const) if (v(k)) next.set(k, v(k));
        if (draft.superuser) next.set("superuser", "true");
        return next;
      },
      { replace: true },
    );
    focusSoon("#audit-count");
  };

  const clear = () => {
    setParams({}, { replace: true });
    setDraft(fromUrl(new URLSearchParams()));
    setDateError(false);
  };

  const exportViews = () => {
    void exporting.run(async () => {
      const body = await api.GET("/api/audit/people/{person}/views", {
        params: { path: { person: subject } },
        parseAs: "text",
      });
      saveText(String(body), `views-${subject}.jsonl`, "application/x-ndjson");
      toast.show(t("core.audit.exported"));
    });
  };

  const actorText = (r: Record_): string => {
    const a = r.actor;
    if (a.kind === "person") {
      if (a.superuser) return t("core.audit.actor.superuser");
      const who = a.person ? nameOf(a.person) : t("core.audit.actor.person");
      const titles = (a.titles ?? []).map((x) =>
        isMessageKey(`core.access.role.${x}.title`) ? t(`core.access.role.${x}.title` as never) : x,
      );
      return titles.length > 0 ? `${who} (${format.list(titles)})` : who;
    }
    return t(`core.audit.actor.${a.kind}`);
  };
  const outcomeText = (o: string) =>
    isMessageKey(`core.audit.outcome.${o}`) ? t(`core.audit.outcome.${o}` as never) : o;

  if (!canSearch)
    return (
      <div className="flex flex-col gap-4">
        <h1 className="text-2xl font-bold">{t("core.audit.title")}</h1>
        <p
          role="alert"
          className="rounded-md border border-danger bg-danger-bg p-4 font-semibold text-danger"
        >
          {t("shell.forbidden")}
        </p>
      </div>
    );

  return (
    <div className="flex flex-col gap-4" aria-busy={q.isPending || undefined}>
      <h1 className="text-2xl font-bold">{t("core.audit.title")}</h1>

      <form ref={formRef} onSubmit={apply} className="flex flex-col gap-4" noValidate>
        <h2 className="text-lg font-bold">{t("core.audit.filters")}</h2>
        <ErrorSummary error={error?.code === "invalid" ? error : null} formRef={formRef} />
        <PersonSearch
          search={searchPeople}
          name="query.subject"
          label={t("core.audit.subject")}
          chosenName={nameOf(subject)}
          hasValue={subject !== ""}
          onPick={(m) => setParam("subject", m.id)}
          onClear={() => setParam("subject", "")}
          error={fieldErrors["query.subject"]}
        />
        <PersonSearch
          search={searchPeople}
          name="query.actor"
          label={t("core.audit.actor_filter")}
          chosenName={nameOf(actor)}
          hasValue={actor !== ""}
          onPick={(m) => setParam("actor", m.id)}
          onClear={() => setParam("actor", "")}
          error={fieldErrors["query.actor"]}
        />
        <Field
          label={t("core.audit.area")}
          name="query.area"
          hint={t("core.audit.area_hint")}
          autoComplete="off"
          value={draft.area}
          onChange={edit("area")}
          error={fieldErrors["query.area"]}
        />
        <Field
          label={t("core.audit.action")}
          name="query.action"
          hint={t("core.audit.action_hint")}
          autoComplete="off"
          value={draft.action}
          onChange={edit("action")}
          error={fieldErrors["query.action"]}
        />
        <Field
          label={t("core.audit.record_kind")}
          name="query.record_kind"
          autoComplete="off"
          value={draft.record_kind}
          onChange={edit("record_kind")}
          error={fieldErrors["query.record_kind"]}
        />
        <Field
          label={t("core.audit.record_id")}
          name="query.record_id"
          hint={t("core.audit.record_hint")}
          autoComplete="off"
          value={draft.record_id}
          onChange={edit("record_id")}
          error={fieldErrors["query.record_id"]}
        />
        <DateField
          label={t("core.audit.from")}
          name="query.from"
          value={draft.from}
          onChange={edit("from")}
          error={
            dateError && draft.from === ""
              ? { text: t("core.audit.dates_together") }
              : fieldErrors["query.from"]
          }
        />
        <DateField
          label={t("core.audit.to")}
          name="query.to"
          value={draft.to}
          onChange={edit("to")}
          error={
            dateError && draft.to === ""
              ? { text: t("core.audit.dates_together") }
              : fieldErrors["query.to"]
          }
        />
        <Check
          name="query.superuser"
          checked={draft.superuser}
          onChange={edit("superuser")}
          label={t("core.audit.superuser")}
        />
        <div className="flex flex-wrap gap-3">
          <Button type="submit">{t("core.audit.apply")}</Button>
          <Button variant="secondary" onClick={clear}>
            {t("core.audit.clear")}
          </Button>
        </div>
      </form>

      {subject && canExport ? (
        <section aria-labelledby="audit-export" className="flex flex-col gap-2">
          <h2 id="audit-export" className="text-lg font-bold">
            {t("core.audit.export_title")}
          </h2>
          <p className="text-muted">{t("core.audit.export_note")}</p>
          {exporting.error ? <ScreenError error={exporting.error} onRetry={exportViews} /> : null}
          <div>
            <Button variant="secondary" busy={exporting.busy} onClick={exportViews}>
              {t("core.audit.export")}
            </Button>
          </div>
        </section>
      ) : null}

      {q.isPending ? <Loading /> : null}
      {error && error.code !== "invalid" ? (
        <ScreenError error={error} onRetry={() => void q.refetch()} />
      ) : null}
      <p id="audit-count" role="status" tabIndex={-1} className="text-sm text-muted">
        {q.data ? t("core.audit.count", { count: records.length }) : ""}
      </p>
      {q.data && records.length === 0 ? <EmptyState>{t("core.audit.empty")}</EmptyState> : null}
      {records.length > 0 ? (
        <ul className="flex flex-col gap-3" aria-label={t("core.audit.list")}>
          {records.map((r) => (
            <li key={r.id} className="rounded-md border border-border bg-surface p-4">
              <p className="break-all text-lg font-bold">{r.action}</p>
              <p className="text-sm text-muted">
                {format.dateTime(r.at)} · {outcomeText(r.outcome)}
              </p>
              <p className="mt-1 break-words">{t("core.audit.by", { actor: actorText(r) })}</p>
              {r.subject ? (
                <p className="break-words">
                  {t("core.audit.about", { subject: nameOf(r.subject) })}
                </p>
              ) : null}
              {r.record ? (
                <p className="break-all text-sm">
                  {t("core.audit.record", { kind: r.record.kind, id: r.record.id })}
                </p>
              ) : null}
              {r.level ? (
                <p className="text-sm">{t("core.audit.level", { level: r.level })}</p>
              ) : null}
              {r.fields && r.fields.length > 0 ? (
                <p className="break-words text-sm">
                  {t("core.audit.fields", { fields: format.list(r.fields) })}
                </p>
              ) : null}
              {r.reason ? (
                <p className="break-words text-sm">
                  {t("core.audit.reason", { reason: r.reason })}
                </p>
              ) : null}
            </li>
          ))}
        </ul>
      ) : null}
      {q.hasNextPage ? (
        <div>
          <Button
            variant="secondary"
            busy={q.isFetchingNextPage}
            onClick={() => void q.fetchNextPage()}
          >
            {t("core.audit.more")}
          </Button>
        </div>
      ) : null}
    </div>
  );
}
