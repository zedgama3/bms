import { useRef, useState } from "react";
import type { FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useLocation, useParams } from "react-router";
import { api, ApiError, useReadOnly, useSession } from "@/api";
import { useFormat, useT } from "@/i18n";
import { Banner, Button, Dialog, ErrorSummary, Loading, useFieldErrors, useToast } from "@/ui";
import { EventFields, readEvent } from "../EventFields";
import { focusSoon } from "../focus";
import { holdsSite } from "../helpers";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { ScheduleSection } from "../ScheduleSection";
import { ScreenError } from "../ScreenError";
import { parseSetup, SetupSection } from "../SetupSection";
import { useAction } from "../useAction";
import { useKindLabel } from "../useKindLabel";

type Dialogs = "edit" | "close" | "reopen" | null;

/** `/events/:event`: the event, its status, the Set-up card, the Schedule, and (with
 *  `events.manage`) edit, close and reopen. A closed event shows a banner and only Reopen. */
export function EventPage() {
  const t = useT();
  const format = useFormat();
  const toast = useToast();
  const client = useQueryClient();
  const session = useSession();
  const { readOnly } = useReadOnly();
  const { event: id = "" } = useParams();
  const location = useLocation();
  const [open, setOpen] = useState<Dialogs>(null);
  const [notEnded, setNotEnded] = useState(false);
  const editRef = useRef<HTMLFormElement>(null);
  const confirmRef = useRef<HTMLFormElement>(null);
  const reopenRef = useRef<HTMLFormElement>(null);
  const act = useAction();
  const fieldErrors = useFieldErrors(act.error);
  const canManage = holdsSite(session, "events.manage");

  const q = useQuery({
    queryKey: ["events", "one", id],
    queryFn: ({ signal }) =>
      api.GET("/api/events/{event}", { signal, params: { path: { event: id } } }),
  });
  const kindsQ = useQuery({
    queryKey: ["events", "kinds"],
    queryFn: ({ signal }) => api.GET("/api/events/kinds", { signal }),
  });
  const kinds = kindsQ.data?.kinds ?? [];
  const kindLabel = useKindLabel(kinds);
  const e = q.data;
  const error = q.error instanceof ApiError ? q.error : null;
  const roDescribe = readOnly ? { "aria-describedby": "event-readonly" } : {};

  const close = () => setOpen(null);
  const done = (message: string) => {
    toast.show(message);
    close();
    void client.invalidateQueries({ queryKey: ["events"] });
    focusSoon("main h1");
  };
  const openDialog = (which: Dialogs) => {
    act.clear();
    setOpen(which);
  };

  const save = (ev: FormEvent<HTMLFormElement>) => {
    ev.preventDefault();
    const v = readEvent(new FormData(ev.currentTarget));
    void act.run(async () => {
      await api.PUT("/api/events/{event}", {
        params: { path: { event: id } },
        body: { name: v.name, kind: v.kind, dates: v.dates, zone: v.zone },
      });
      done(t("core.events.page.saved"));
    });
  };
  const toggle = (which: "close" | "reopen") => (ev: FormEvent<HTMLFormElement>) => {
    ev.preventDefault();
    void act.run(async () => {
      await api.POST(
        which === "close" ? "/api/events/{event}/close" : "/api/events/{event}/reopen",
        { params: { path: { event: id } } },
      );
      done(
        which === "close" ? t("core.events.page.closed_done") : t("core.events.page.reopened_done"),
      );
    });
  };

  return (
    <div className="flex flex-col gap-4" aria-busy={q.isPending || undefined}>
      <div>
        <Link to="/events" className="inline-flex min-h-11 items-center underline">
          {t("core.events.page.back")}
        </Link>
      </div>
      {q.isPending ? <Loading /> : null}
      {error ? (
        <>
          <h1 className="text-2xl font-bold">{t("core.events.page.title")}</h1>
          <ScreenError error={error} onRetry={() => void q.refetch()} />
        </>
      ) : null}
      {e ? (
        <>
          <h1 className="break-words text-2xl font-bold">{e.name}</h1>
          {e.closed ? (
            <Banner tone="warn">
              {e.closed_at
                ? t("core.events.page.closed_on", { date: format.date(e.closed_at, e.zone) })
                : t("core.events.page.closed")}
            </Banner>
          ) : null}
          <dl className="flex flex-col gap-2 rounded-md border border-border bg-surface p-4">
            <div>
              <dt className="text-sm text-muted">{t("core.events.field.kind")}</dt>
              <dd className="font-semibold">{kindLabel(e.kind)}</dd>
            </div>
            <div>
              <dt className="text-sm text-muted">{t("core.events.page.dates")}</dt>
              <dd className="font-semibold">
                {t("core.events.dates", {
                  start: format.dateTime(e.dates.start, e.zone),
                  end: format.dateTime(e.dates.end, e.zone),
                })}
              </dd>
            </div>
            <div>
              <dt className="text-sm text-muted">{t("core.events.field.zone")}</dt>
              <dd className="font-semibold">{e.zone}</dd>
            </div>
            <div>
              <dt className="text-sm text-muted">{t("core.events.page.status")}</dt>
              <dd className="font-semibold">
                {e.closed ? t("core.events.closed_badge") : t("core.events.page.open")}
              </dd>
            </div>
          </dl>

          {canManage ? (
            <section aria-labelledby="event-actions" className="flex flex-col gap-3">
              <h2 id="event-actions" className="sr-only">
                {t("core.events.page.actions")}
              </h2>
              <div className="flex flex-wrap gap-3">
                {e.closed ? (
                  <Button disabled={readOnly} {...roDescribe} onClick={() => openDialog("reopen")}>
                    {t("core.events.page.reopen")}
                  </Button>
                ) : (
                  <>
                    <Button
                      variant="secondary"
                      disabled={readOnly}
                      {...roDescribe}
                      onClick={() => openDialog("edit")}
                    >
                      {t("core.events.page.edit")}
                    </Button>
                    <Button
                      variant="danger"
                      disabled={readOnly}
                      {...roDescribe}
                      onClick={() => {
                        setNotEnded(Date.parse(e.dates.end) > Date.now());
                        openDialog("close");
                      }}
                    >
                      {t("core.events.page.close")}
                    </Button>
                  </>
                )}
              </div>
              <ReadOnlyNote id="event-readonly" readOnly={readOnly} />
            </section>
          ) : null}

          <SetupSection
            eventId={e.id}
            canRun={canManage && !e.closed}
            readOnly={readOnly}
            initial={parseSetup(location.state)}
          />
          <ScheduleSection event={e} canManage={canManage} readOnly={readOnly} />

          <Dialog open={open === "edit"} title={t("core.events.page.edit")} onClose={close}>
            {open === "edit" && kindsQ.data ? (
              <form
                key={e.updated_at}
                ref={editRef}
                onSubmit={save}
                className="flex flex-col gap-4"
                noValidate
              >
                <ErrorSummary error={act.error} formRef={editRef} />
                <EventFields event={e} kinds={kinds} kindLabel={kindLabel} errors={fieldErrors} />
                <div className="flex flex-wrap gap-3">
                  <Button type="submit" busy={act.busy}>
                    {t("core.events.page.save")}
                  </Button>
                  <Button variant="secondary" onClick={close}>
                    {t("core.events.cancel")}
                  </Button>
                </div>
              </form>
            ) : null}
            {open === "edit" && kindsQ.isPending ? <Loading /> : null}
            {open === "edit" && kindsQ.error instanceof ApiError ? (
              <ScreenError error={kindsQ.error} onRetry={() => void kindsQ.refetch()} />
            ) : null}
          </Dialog>

          <Dialog open={open === "close"} title={t("core.events.page.close_title")} onClose={close}>
            <form ref={confirmRef} onSubmit={toggle("close")} className="flex flex-col gap-4">
              {open === "close" ? <ErrorSummary error={act.error} formRef={confirmRef} /> : null}
              <p>{t("core.events.page.close_body", { name: e.name })}</p>
              {notEnded ? <p className="font-semibold">{t("core.events.page.not_ended")}</p> : null}
              <div className="flex flex-wrap gap-3">
                <Button type="submit" variant="danger" busy={act.busy}>
                  {t("core.events.page.close_confirm")}
                </Button>
                <Button variant="secondary" onClick={close}>
                  {t("core.events.cancel")}
                </Button>
              </div>
            </form>
          </Dialog>

          <Dialog
            open={open === "reopen"}
            title={t("core.events.page.reopen_title")}
            onClose={close}
          >
            <form ref={reopenRef} onSubmit={toggle("reopen")} className="flex flex-col gap-4">
              {open === "reopen" ? <ErrorSummary error={act.error} formRef={reopenRef} /> : null}
              <p>{t("core.events.page.reopen_body", { name: e.name })}</p>
              <div className="flex flex-wrap gap-3">
                <Button type="submit" busy={act.busy}>
                  {t("core.events.page.reopen_confirm")}
                </Button>
                <Button variant="secondary" onClick={close}>
                  {t("core.events.cancel")}
                </Button>
              </div>
            </form>
          </Dialog>
        </>
      ) : null}
    </div>
  );
}
