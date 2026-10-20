import { useQueryClient } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { useEffect, useRef, useState } from "react";
import { Link } from "react-router";
import { api, ApiError, useReadOnly, useSession } from "@/api";
import type { components } from "@/api";
import { isMessageKey, useFormat, useT } from "@/i18n";
import { Button, EmptyState, ErrorState, linkButtonClass, Loading, Table, useToast } from "@/ui";
import type { Column } from "@/ui";
import { ConfirmDialog } from "../ConfirmDialog";
import { Forbidden } from "../Forbidden";
import { focusSoon } from "../focus";
import { fullName, holdsSite, passed } from "../helpers";
import { OUTBOX_KEY, useOutbox } from "../queries";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { ScreenError } from "../ScreenError";
import { useAction } from "../useAction";
import { useNames } from "../useNames";
import { useTemplateText } from "../useTemplateText";

type Row = components["schemas"]["NotifyRecord"];

const STATUSES = ["queued", "sending", "sent", "failed"] as const;

/** `/notify/outbox` (`notify.outbox.manage`): whether e-mail is set up, a connection check, the
 *  count of messages in each state, and the newest queued, sending and failed ones. A row shows
 *  its ID and tokens only: never an address, a subject or a body. */
export function Outbox() {
  const t = useT();
  const format = useFormat();
  const text = useTemplateText();
  const session = useSession();
  const toast = useToast();
  const client = useQueryClient();
  const { readOnly } = useReadOnly();
  const allowed = holdsSite(session, "notify.outbox.manage");
  const q = useOutbox(allowed);
  const nameOf = useNames(
    (q.data?.recent ?? []).flatMap((r) => (r.person_id ? [r.person_id] : [])),
  );
  const check = useAction();
  const [checked, setChecked] = useState(false);
  const row = useAction();
  const [rowId, setRowId] = useState<string | null>(null); // the row the last retry was for
  const checkBox = useRef<HTMLParagraphElement>(null);
  useEffect(() => {
    if (checked) checkBox.current?.focus();
  }, [checked]);
  const cancel = useAction();
  const [canceling, setCanceling] = useState<string | null>(null);

  if (!allowed) return <Forbidden title={t("core.notify.outbox.title")} />;

  const error = q.error instanceof ApiError ? q.error : null;
  const data = q.data;
  const refresh = () => client.invalidateQueries({ queryKey: OUTBOX_KEY });

  const runCheck = () => {
    setChecked(false);
    void check.run(async () => {
      await api.POST("/api/notify/outbox/check");
      setChecked(true);
    });
  };

  const retry = (id: string) => {
    setRowId(id);
    void row
      .run(async () => {
        await api.POST("/api/notify/outbox/{message}/retry", { params: { path: { message: id } } });
        toast.show(t("core.notify.outbox.retried"));
      })
      .then((ok) => {
        void refresh();
        if (ok) focusSoon("#outbox-title"); // the row changes and the pressed button is gone
      });
  };

  const doCancel = () => {
    const id = canceling;
    if (id === null) return;
    void cancel
      .run(async () => {
        await api.POST("/api/notify/outbox/{message}/cancel", {
          params: { path: { message: id } },
        });
        toast.show(t("core.notify.outbox.canceled"));
      })
      .then((ok) => {
        void refresh();
        if (ok) {
          setCanceling(null);
          focusSoon("#outbox-title"); // the row is gone with the dialog
        }
      });
  };

  /** The sentence for a failed retry: the server's `expired` has its own. */
  const rowMessage = (e: ApiError): string | undefined => {
    if (e.code === ("expired" as string) || e.fields.some((f) => f.code === "expired"))
      return t("core.notify.outbox.expired_error");
    if (e.code === "conflict") return t("core.notify.outbox.conflict");
    return undefined;
  };

  /** A known token in words; an unknown one as it came. */
  const errorText = (token: string): ReactNode => {
    const key = `core.notify.error.${token}`;
    return isMessageKey(key) ? t(key) : <code translate="no">{token}</code>;
  };
  const templateText = (name: string): ReactNode => {
    const title = text.title(name);
    return title === name ? <code translate="no">{name}</code> : title;
  };

  const columns: Column<Row>[] = [
    {
      header: t("core.notify.outbox.col.id"),
      rowHeader: true,
      cell: (r) => <code className="break-all">{r.id}</code>,
    },
    {
      header: t("core.notify.outbox.col.recipient"),
      cell: (r) => (r.person_id ? nameOf(r.person_id) : t("core.notify.outbox.recipient.address")),
    },
    {
      header: t("core.notify.outbox.col.template"),
      cell: (r) => templateText(fullName(r.owner, r.template)),
    },
    {
      header: t("core.notify.outbox.col.status"),
      cell: (r) => t(`core.notify.status.${r.status}`),
    },
    { header: t("core.notify.outbox.col.attempts"), cell: (r) => format.number(r.attempts) },
    {
      header: t("core.notify.outbox.col.error"),
      cell: (r) => (r.last_error ? errorText(r.last_error) : t("core.notify.outbox.none")),
    },
    { header: t("core.notify.outbox.col.created"), cell: (r) => format.dateTime(r.created_at) },
    {
      header: t("core.notify.outbox.col.expires"),
      cell: (r) =>
        r.expires_at
          ? passed(r.expires_at)
            ? t("core.notify.outbox.expired")
            : format.dateTime(r.expires_at)
          : t("core.notify.outbox.none"),
    },
    {
      header: t("core.notify.outbox.col.actions"),
      cell: (r) => {
        const expired = passed(r.expires_at) || r.last_error === "expired";
        const describe = readOnly ? { "aria-describedby": "outbox-readonly" } : {};
        const rowError = row.error && rowId === r.id ? row.error : null;
        return (
          <div className="flex flex-col gap-2">
            <div className="flex flex-wrap gap-2">
              {r.status === "failed" ? (
                <Button
                  variant="secondary"
                  busy={row.busy && rowId === r.id}
                  disabled={readOnly || expired || (row.busy && rowId !== r.id)}
                  {...describe}
                  aria-label={t("core.notify.outbox.retry_label", { id: r.id })}
                  onClick={() => retry(r.id)}
                >
                  {t("core.notify.outbox.retry")}
                </Button>
              ) : null}
              {r.status === "queued" || r.status === "failed" ? (
                <Button
                  variant="danger"
                  disabled={readOnly}
                  {...describe}
                  aria-label={t("core.notify.outbox.cancel_label", { id: r.id })}
                  onClick={() => {
                    cancel.clear();
                    setCanceling(r.id);
                  }}
                >
                  {t("core.notify.outbox.cancel")}
                </Button>
              ) : null}
            </div>
            {rowError ? <ErrorState error={rowError} {...msg(rowMessage(rowError))} /> : null}
          </div>
        );
      },
    },
  ];

  return (
    <div className="flex flex-col gap-6" aria-busy={q.isPending || undefined}>
      <h1 id="outbox-title" tabIndex={-1} className="text-2xl font-bold focus:outline-none">
        {t("core.notify.outbox.title")}
      </h1>
      {q.isPending ? <Loading /> : null}
      {error ? <ScreenError error={error} onRetry={() => void q.refetch()} /> : null}
      {data ? (
        <>
          <section aria-labelledby="outbox-status" className="flex flex-col gap-3">
            <h2 id="outbox-status" className="text-xl font-bold">
              {t("core.notify.outbox.status_title")}
            </h2>
            <p className="font-semibold">
              {data.configured
                ? t("core.notify.outbox.configured")
                : t("core.notify.not_configured")}
            </p>
            {data.configured ? null : (
              <div>
                <Link to="/settings/notify" className={linkButtonClass("secondary")}>
                  {t("core.notify.open_settings")}
                </Link>
              </div>
            )}
            <dl className="grid grid-cols-2 gap-3 sm:grid-cols-4">
              {STATUSES.map((s) => (
                <div key={s} className="rounded-md border border-border p-3">
                  <dt className="text-sm text-muted">{t(`core.notify.status.${s}`)}</dt>
                  <dd className="text-2xl font-bold">{format.number(data.counts[s] ?? 0)}</dd>
                </div>
              ))}
            </dl>
            <div className="flex flex-wrap gap-3">
              <Button
                variant="secondary"
                busy={check.busy}
                disabled={!data.configured}
                onClick={runCheck}
              >
                {t("core.notify.outbox.check")}
              </Button>
              <Button variant="secondary" onClick={() => void q.refetch()}>
                {t("core.notify.outbox.refresh")}
              </Button>
            </div>
            {checked ? (
              <p
                ref={checkBox}
                role="status"
                tabIndex={-1}
                className="font-semibold focus:outline-none"
              >
                {t("core.notify.outbox.check_ok")}
              </p>
            ) : null}
            {check.error ? (
              <ErrorState
                error={check.error}
                {...(check.error.code === "unsupported"
                  ? { message: t("core.notify.not_configured") }
                  : {})}
              />
            ) : null}
          </section>

          <section aria-labelledby="outbox-recent" className="flex flex-col gap-3">
            <h2 id="outbox-recent" className="text-xl font-bold">
              {t("core.notify.outbox.recent_title")}
            </h2>
            <ReadOnlyNote id="outbox-readonly" readOnly={readOnly} />
            {data.recent.length === 0 ? (
              <EmptyState>{t("core.notify.outbox.empty")}</EmptyState>
            ) : (
              <Table
                caption={t("core.notify.outbox.caption")}
                columns={columns}
                rows={data.recent}
                rowKey={(r) => r.id}
              />
            )}
          </section>
        </>
      ) : null}
      <ConfirmDialog
        open={canceling !== null}
        title={t("core.notify.outbox.cancel_title")}
        body={t("core.notify.outbox.cancel_body")}
        confirm={t("core.notify.outbox.cancel")}
        busy={cancel.busy}
        error={cancel.error}
        {...msg(cancel.error ? rowMessage(cancel.error) : undefined)}
        onConfirm={doCancel}
        onClose={() => setCanceling(null)}
      />
    </div>
  );
}

function msg(m: string | undefined): { message?: string } {
  return m === undefined ? {} : { message: m };
}
