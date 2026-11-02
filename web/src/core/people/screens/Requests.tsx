import { useRef, useState } from "react";
import type { FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";
import { api, ApiError, useAction, useNames, useReadOnly } from "@/api";
import { useFormat, useT } from "@/i18n";
import type { MessageKey } from "@/i18n";
import {
  Button,
  controlClass,
  Dialog,
  EmptyState,
  ErrorSummary,
  Field,
  Loading,
  useToast,
} from "@/ui";
import { focusSoon } from "../focus";
import { DeletionReportView } from "../DeletionReportView";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { ScreenError } from "../ScreenError";
import { text } from "../text";
import type { DataRequest, DeletionReport } from "../types";

type Status = "open" | "declined" | "withdrawn";
const STATUSES: readonly Status[] = ["open", "declined", "withdrawn"];
const STATUS_LABEL: Record<Status, MessageKey> = {
  open: "core.people.requests.status_open",
  declined: "core.people.requests.status_declined",
  withdrawn: "core.people.requests.status_withdrawn",
};
const EMPTY: Record<Status, MessageKey> = {
  open: "core.people.requests.empty_open",
  declined: "core.people.requests.empty_declined",
  withdrawn: "core.people.requests.empty_withdrawn",
};

/** `/people/admin/requests`: deletion requests by status. Fulfil deletes the person (after a
 *  confirmation, then shows what was removed); decline sends a note back. */
export function Requests() {
  const t = useT();
  const format = useFormat();
  const toast = useToast();
  const client = useQueryClient();
  const { readOnly } = useReadOnly();
  const [status, setStatus] = useState<Status>("open");
  const [fulfilling, setFulfilling] = useState<DataRequest | null>(null);
  const [declining, setDeclining] = useState<DataRequest | null>(null);
  const [report, setReport] = useState<DeletionReport | null>(null);
  const fulfilRef = useRef<HTMLFormElement>(null);
  const declineRef = useRef<HTMLFormElement>(null);
  const fulfil = useAction();
  const decline = useAction();

  const q = useQuery({
    queryKey: ["people", "requests", status],
    queryFn: ({ signal }) =>
      api.GET("/api/datarights/requests", { signal, params: { query: { status } } }),
  });
  const error = q.error instanceof ApiError ? q.error : null;
  const requests = q.data?.requests ?? [];
  const nameOf = useNames(requests.map((r) => r.person_id));
  const describedBy = readOnly ? { "aria-describedby": "requests-readonly" } : {};

  const refresh = () => client.invalidateQueries({ queryKey: ["people", "requests"] });

  const submitFulfil = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const r = fulfilling;
    if (!r) return;
    void fulfil.run(async () => {
      const res = await api.POST("/api/datarights/requests/{request}/fulfil", {
        params: { path: { request: r.id } },
      });
      setFulfilling(null);
      setReport(res);
      await refresh();
    });
  };

  const submitDecline = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const r = declining;
    if (!r) return;
    const note = text(new FormData(e.currentTarget).get("note")).trim();
    void decline.run(async () => {
      await api.POST("/api/datarights/requests/{request}/decline", {
        params: { path: { request: r.id } },
        body: { ...(note ? { note } : {}) },
      });
      setDeclining(null);
      toast.show(t("core.people.requests.declined"));
      await refresh();
      focusSoon("main h1");
    });
  };

  return (
    <div className="flex flex-col gap-4" aria-busy={q.isPending || undefined}>
      <h1 className="text-2xl font-bold">{t("core.people.requests.title")}</h1>
      <div
        role="group"
        aria-label={t("core.people.requests.status")}
        className="flex flex-wrap gap-2"
      >
        {STATUSES.map((s) => (
          <Button
            key={s}
            variant={s === status ? "primary" : "secondary"}
            aria-pressed={s === status}
            onClick={() => setStatus(s)}
          >
            {t(STATUS_LABEL[s])}
          </Button>
        ))}
      </div>
      {q.isPending ? <Loading /> : null}
      {error ? <ScreenError error={error} onRetry={() => void q.refetch()} /> : null}
      {q.data && requests.length === 0 ? <EmptyState>{t(EMPTY[status])}</EmptyState> : null}
      {requests.length > 0 ? (
        <ul className="flex flex-col gap-3" aria-label={t("core.people.requests.list")}>
          {requests.map((r) => (
            <li key={r.id} className="rounded-md border border-border bg-surface p-4">
              <h2 className="break-words text-lg font-bold">
                <Link
                  to={`/people/${r.person_id}`}
                  className="inline-flex min-h-11 items-center underline"
                >
                  {nameOf(r.person_id)}
                </Link>
              </h2>
              <p>{t("core.people.requests.asked", { date: format.date(r.created_at) })}</p>
              {r.note ? <p className="break-words">{r.note}</p> : null}
              {r.status !== "open" && r.decision_note ? (
                <p className="break-words text-muted">
                  {t("core.people.requests.decision", { note: r.decision_note })}
                </p>
              ) : null}
              {r.status === "open" ? (
                <div className="mt-2 flex flex-wrap gap-3">
                  <Button
                    variant="danger"
                    disabled={readOnly}
                    {...describedBy}
                    onClick={() => {
                      fulfil.clear();
                      setFulfilling(r);
                    }}
                  >
                    {t("core.people.requests.fulfil")}
                  </Button>
                  <Button
                    variant="secondary"
                    disabled={readOnly}
                    {...describedBy}
                    onClick={() => {
                      decline.clear();
                      setDeclining(r);
                    }}
                  >
                    {t("core.people.requests.decline")}
                  </Button>
                </div>
              ) : null}
            </li>
          ))}
        </ul>
      ) : null}
      <ReadOnlyNote id="requests-readonly" readOnly={readOnly} />

      <Dialog
        open={fulfilling !== null}
        title={t("core.people.requests.fulfil_title")}
        onClose={() => setFulfilling(null)}
      >
        <form ref={fulfilRef} onSubmit={submitFulfil} className="flex flex-col gap-4">
          <ErrorSummary error={fulfil.error} formRef={fulfilRef} />
          <p className="break-words font-semibold">
            {fulfilling ? nameOf(fulfilling.person_id) : ""}
          </p>
          <p>{t("core.people.requests.fulfil_body")}</p>
          <div className="flex flex-wrap gap-3">
            <Button type="submit" variant="danger" busy={fulfil.busy}>
              {t("core.people.requests.fulfil_confirm")}
            </Button>
            <Button variant="secondary" onClick={() => setFulfilling(null)}>
              {t("core.people.cancel")}
            </Button>
          </div>
        </form>
      </Dialog>

      <Dialog
        open={declining !== null}
        title={t("core.people.requests.decline_title")}
        onClose={() => setDeclining(null)}
      >
        <form ref={declineRef} onSubmit={submitDecline} className="flex flex-col gap-4" noValidate>
          <ErrorSummary error={decline.error} formRef={declineRef} />
          <Field
            label={t("core.people.requests.decline_note")}
            name="note"
            hint={t("core.people.requests.decline_hint")}
          >
            {(control) => (
              <textarea
                {...control}
                rows={3}
                maxLength={500}
                autoComplete="off"
                className={controlClass}
              />
            )}
          </Field>
          <div className="flex flex-wrap gap-3">
            <Button type="submit" busy={decline.busy}>
              {t("core.people.requests.decline_confirm")}
            </Button>
            <Button variant="secondary" onClick={() => setDeclining(null)}>
              {t("core.people.cancel")}
            </Button>
          </div>
        </form>
      </Dialog>

      <Dialog
        open={report !== null}
        title={t("core.people.requests.report_title")}
        onClose={() => {
          setReport(null);
          focusSoon("main h1");
        }}
      >
        {report ? <DeletionReportView report={report} /> : null}
      </Dialog>
    </div>
  );
}
