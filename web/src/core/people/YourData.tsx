import { useRef, useState } from "react";
import type { FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, useAction, useReadOnly } from "@/api";
import { useFormat, useT } from "@/i18n";
import {
  Button,
  Dialog,
  ErrorState,
  ErrorSummary,
  Field,
  Loading,
  controlClass,
  useFieldErrors,
  useToast,
} from "@/ui";
import { saveJson } from "./download";
import { ReadOnlyNote } from "./ReadOnlyNote";
import { text } from "./text";

const KEY = ["people", "my-requests"] as const;

/** "Your data": download everything held about you, ask for it to be deleted, withdraw the
 *  request. A self download can run once every ten minutes; a refusal says when to try again. */
export function YourData() {
  const t = useT();
  const format = useFormat();
  const toast = useToast();
  const client = useQueryClient();
  const { readOnly } = useReadOnly();
  const [asking, setAsking] = useState(false);
  const formRef = useRef<HTMLFormElement>(null);
  const exporting = useAction();
  const request = useAction();
  const withdraw = useAction();
  const fieldErrors = useFieldErrors(request.error);

  const q = useQuery({
    queryKey: KEY,
    queryFn: ({ signal }) => api.GET("/api/people/me/requests", { signal }),
  });
  const error = q.error instanceof ApiError ? q.error : null;
  const requests = q.data?.requests ?? [];
  const open = requests.find((r) => r.status === "open");
  const declined = requests.find((r) => r.status === "declined");
  const describedBy = readOnly ? { "aria-describedby": "data-readonly" } : {};

  const download = () => {
    void exporting.run(async () => {
      const data = await api.GET("/api/people/me/export");
      saveJson(data, "bms-export.json");
      toast.show(t("core.people.data.downloaded"));
    });
  };

  const ask = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const note = text(new FormData(e.currentTarget).get("note")).trim();
    void request.run(async () => {
      await api.POST("/api/people/me/requests", {
        body: { kind: "delete", ...(note ? { note } : {}) },
      });
      setAsking(false);
      toast.show(t("core.people.data.requested"));
      await client.invalidateQueries({ queryKey: KEY });
    });
  };

  const cancel = (id: string) => {
    void withdraw.run(async () => {
      await api.POST("/api/people/me/requests/{request}/withdraw", {
        params: { path: { request: id } },
      });
      toast.show(t("core.people.data.withdrawn"));
      await client.invalidateQueries({ queryKey: KEY });
    });
  };

  // A refused download says when to try again (429 with Retry-After), in minutes.
  const exportError = exporting.error;
  const cooling = exportError?.code === "rate_limited";
  const minutes = Math.max(1, Math.ceil((exportError?.retryAfter ?? 600) / 60));

  return (
    <section aria-labelledby="data-h" className="flex flex-col gap-4">
      <h2 id="data-h" className="text-xl font-bold">
        {t("core.people.data.title")}
      </h2>

      <div className="flex flex-col gap-2">
        <h3 className="font-semibold">{t("core.people.data.download_title")}</h3>
        <p className="text-muted">{t("core.people.data.download_body")}</p>
        {cooling ? (
          <p role="alert" className="font-semibold text-danger">
            {t("core.people.data.cooldown", { minutes })}
          </p>
        ) : exportError ? (
          <ErrorState error={exportError} />
        ) : null}
        <div>
          <Button variant="secondary" busy={exporting.busy} onClick={download}>
            {t("core.people.data.download")}
          </Button>
        </div>
      </div>

      <div className="flex flex-col gap-2">
        <h3 className="font-semibold">{t("core.people.data.delete_title")}</h3>
        <p className="text-muted">{t("core.people.data.delete_body")}</p>
        {q.isPending ? <Loading /> : null}
        {error ? <ErrorState error={error} onRetry={() => void q.refetch()} /> : null}
        {withdraw.error ? <ErrorState error={withdraw.error} /> : null}
        {open ? (
          <div className="flex flex-col gap-2 rounded-md border border-border bg-surface p-4">
            <p className="font-semibold">
              {t("core.people.data.open", { date: format.date(open.created_at) })}
            </p>
            {open.note ? <p className="break-words">{open.note}</p> : null}
            <div>
              <Button
                variant="secondary"
                busy={withdraw.busy}
                disabled={readOnly}
                {...describedBy}
                onClick={() => cancel(open.id)}
              >
                {t("core.people.data.withdraw")}
              </Button>
            </div>
          </div>
        ) : q.data ? (
          <>
            {declined ? (
              <div className="rounded-md border border-border bg-surface p-4">
                <p className="font-semibold">
                  {t("core.people.data.declined", {
                    date: format.date(declined.decided_at ?? declined.created_at),
                  })}
                </p>
                {declined.decision_note ? (
                  <p className="break-words">{declined.decision_note}</p>
                ) : null}
              </div>
            ) : null}
            <div>
              <Button
                variant="danger"
                disabled={readOnly}
                {...describedBy}
                onClick={() => {
                  request.clear();
                  setAsking(true);
                }}
              >
                {t("core.people.data.request")}
              </Button>
            </div>
          </>
        ) : null}
        <ReadOnlyNote id="data-readonly" readOnly={readOnly} />
      </div>

      <Dialog
        open={asking}
        title={t("core.people.data.request_title")}
        onClose={() => setAsking(false)}
      >
        <form ref={formRef} onSubmit={ask} className="flex flex-col gap-4" noValidate>
          <ErrorSummary error={request.error} formRef={formRef} />
          <p>{t("core.people.data.request_body")}</p>
          <Field label={t("core.people.data.note")} name="note" error={fieldErrors["note"]}>
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
            <Button type="submit" variant="danger" busy={request.busy}>
              {t("core.people.data.request_confirm")}
            </Button>
            <Button variant="secondary" onClick={() => setAsking(false)}>
              {t("core.people.cancel")}
            </Button>
          </div>
        </form>
      </Dialog>
    </section>
  );
}
