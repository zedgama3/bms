import { useRef, useState } from "react";
import type { FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { TriangleAlert } from "lucide-react";
import { api } from "@/api";
import { useFormat, useT } from "@/i18n";
import {
  Button,
  controlClass,
  Dialog,
  EmptyState,
  ErrorSummary,
  Field,
  useFieldErrors,
  useToast,
} from "@/ui";
import { focusSoon } from "./focus";
import { instantToLocal, localToInstant } from "./helpers";
import { ReadOnlyNote } from "./ReadOnlyNote";
import { text } from "./text";
import type { EventDetail, ScheduleItem } from "./types";
import { useAction } from "./useAction";

const ZERO_INSTANT = "0001-01-01T00:00:00Z";

type Editing = { kind: "add" } | { kind: "edit"; item: ScheduleItem } | null;

/** The Schedule list of an event page: each item's moment or window in the event's zone, a
 *  warning for one outside the event's dates, and (with `events.manage` on an open event) add,
 *  edit and delete. */
export function ScheduleSection({
  event,
  canManage,
  readOnly,
}: {
  event: EventDetail;
  canManage: boolean;
  readOnly: boolean;
}) {
  const t = useT();
  const format = useFormat();
  const toast = useToast();
  const client = useQueryClient();
  const [editing, setEditing] = useState<Editing>(null);
  const [deleting, setDeleting] = useState<ScheduleItem | null>(null);
  const formRef = useRef<HTMLFormElement>(null);
  const deleteRef = useRef<HTMLFormElement>(null);
  const act = useAction();
  const fieldErrors = useFieldErrors(act.error);
  const editable = canManage && !event.closed;
  const evStart = Date.parse(event.dates.start);
  const evEnd = Date.parse(event.dates.end);

  const outside = (i: ScheduleItem) => {
    const s = Date.parse(i.starts_at);
    return s < evStart || s >= evEnd || (i.ends_at !== undefined && Date.parse(i.ends_at) > evEnd);
  };
  const when = (i: ScheduleItem) =>
    i.ends_at
      ? t("core.events.schedule.window", {
          start: format.dateTime(i.starts_at, event.zone),
          end: format.dateTime(i.ends_at, event.zone),
        })
      : t("core.events.schedule.moment", { at: format.dateTime(i.starts_at, event.zone) });
  const roDescribe = readOnly ? { "aria-describedby": "schedule-readonly" } : {};

  const done = (message: string) => {
    toast.show(message);
    setEditing(null);
    setDeleting(null);
    void client.invalidateQueries({ queryKey: ["events"] });
    focusSoon("#event-schedule");
  };

  const save = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const data = new FormData(e.currentTarget);
    const v = (name: string) => text(data.get(name)).trim();
    const key = v("key");
    const endsAt = localToInstant(v("ends_at"), event.zone);
    const body = {
      label: v("label"),
      starts_at: localToInstant(v("starts_at"), event.zone) || ZERO_INSTANT,
      ...(key ? { key } : {}),
      ...(endsAt ? { ends_at: endsAt } : {}),
    };
    const current = editing;
    void act.run(async () => {
      if (current?.kind === "edit") {
        await api.PUT("/api/events/{event}/schedule/{item}", {
          params: { path: { event: event.id, item: current.item.id } },
          body,
        });
        done(t("core.events.schedule.saved"));
      } else {
        await api.POST("/api/events/{event}/schedule", {
          params: { path: { event: event.id } },
          body,
        });
        done(t("core.events.schedule.added"));
      }
    });
  };

  const remove = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const item = deleting;
    if (!item) return;
    void act.run(async () => {
      await api.DELETE("/api/events/{event}/schedule/{item}", {
        params: { path: { event: event.id, item: item.id } },
      });
      done(t("core.events.schedule.deleted"));
    });
  };

  const item = editing?.kind === "edit" ? editing.item : null;

  return (
    <section aria-labelledby="event-schedule" className="flex flex-col gap-3">
      <h2 id="event-schedule" tabIndex={-1} className="text-xl font-bold">
        {t("core.events.schedule.title")}
      </h2>
      {editable ? (
        <div className="flex flex-col gap-1">
          <div>
            <Button
              disabled={readOnly}
              {...roDescribe}
              onClick={() => {
                act.clear();
                setEditing({ kind: "add" });
              }}
            >
              {t("core.events.schedule.add")}
            </Button>
          </div>
          <ReadOnlyNote id="schedule-readonly" readOnly={readOnly} />
        </div>
      ) : null}
      {event.schedule.length === 0 ? (
        <EmptyState>{t("core.events.schedule.empty")}</EmptyState>
      ) : (
        <ul className="flex flex-col gap-3">
          {event.schedule.map((i) => (
            <li key={i.id} className="rounded-md border border-border bg-surface p-4">
              <h3 className="break-words text-lg font-bold">{i.label}</h3>
              <p>{when(i)}</p>
              {i.key ? (
                <p className="text-sm text-muted">
                  {t("core.events.schedule.key")} <code>{i.key}</code>
                </p>
              ) : null}
              {outside(i) ? (
                <p className="mt-1 flex items-start gap-2 font-semibold text-warn-fg">
                  <TriangleAlert aria-hidden="true" className="mt-1 size-5 shrink-0" />
                  <span>{t("core.events.schedule.outside")}</span>
                </p>
              ) : null}
              {editable ? (
                <div className="mt-2 flex flex-wrap gap-3">
                  <Button
                    variant="secondary"
                    disabled={readOnly}
                    {...roDescribe}
                    onClick={() => {
                      act.clear();
                      setEditing({ kind: "edit", item: i });
                    }}
                  >
                    {t("core.events.schedule.edit")} <span className="sr-only">{i.label}</span>
                  </Button>
                  <Button
                    variant="danger"
                    disabled={readOnly}
                    {...roDescribe}
                    onClick={() => {
                      act.clear();
                      setDeleting(i);
                    }}
                  >
                    {t("core.events.schedule.delete")} <span className="sr-only">{i.label}</span>
                  </Button>
                </div>
              ) : null}
            </li>
          ))}
        </ul>
      )}

      <Dialog
        open={editing !== null}
        title={item ? t("core.events.schedule.edit_title") : t("core.events.schedule.add")}
        onClose={() => setEditing(null)}
      >
        {editing ? (
          <form ref={formRef} onSubmit={save} className="flex flex-col gap-4" noValidate>
            <ErrorSummary error={act.error} formRef={formRef} />
            <Field
              label={t("core.events.schedule.label")}
              name="label"
              required
              maxLength={100}
              autoComplete="off"
              defaultValue={item?.label ?? ""}
              error={fieldErrors["label"]}
            />
            <Field
              label={t("core.events.schedule.key_label")}
              name="key"
              hint={t("core.events.schedule.key_hint")}
              error={fieldErrors["key"]}
            >
              {(control) => (
                <input
                  {...control}
                  className={controlClass}
                  type="text"
                  maxLength={32}
                  autoComplete="off"
                  autoCapitalize="none"
                  autoCorrect="off"
                  spellCheck={false}
                  defaultValue={item?.key ?? ""}
                />
              )}
            </Field>
            <Field
              label={t("core.events.schedule.starts")}
              name="starts_at"
              type="datetime-local"
              required
              hint={t("core.events.schedule.zone_hint", { zone: event.zone })}
              autoComplete="off"
              defaultValue={item ? instantToLocal(item.starts_at, event.zone) : ""}
              error={fieldErrors["starts_at"]}
            />
            <Field
              label={t("core.events.schedule.ends")}
              name="ends_at"
              type="datetime-local"
              hint={t("core.events.schedule.ends_hint")}
              autoComplete="off"
              defaultValue={item?.ends_at ? instantToLocal(item.ends_at, event.zone) : ""}
              error={fieldErrors["ends_at"]}
            />
            <div className="flex flex-wrap gap-3">
              <Button type="submit" busy={act.busy}>
                {t("core.events.schedule.save")}
              </Button>
              <Button variant="secondary" onClick={() => setEditing(null)}>
                {t("core.events.cancel")}
              </Button>
            </div>
          </form>
        ) : null}
      </Dialog>

      <Dialog
        open={deleting !== null}
        title={t("core.events.schedule.delete_title")}
        onClose={() => setDeleting(null)}
      >
        <form ref={deleteRef} onSubmit={remove} className="flex flex-col gap-4">
          {deleting ? <ErrorSummary error={act.error} formRef={deleteRef} /> : null}
          <p>{deleting ? t("core.events.schedule.delete_body", { label: deleting.label }) : ""}</p>
          <div className="flex flex-wrap gap-3">
            <Button type="submit" variant="danger" busy={act.busy}>
              {t("core.events.schedule.delete_confirm")}
            </Button>
            <Button variant="secondary" onClick={() => setDeleting(null)}>
              {t("core.events.cancel")}
            </Button>
          </div>
        </form>
      </Dialog>
    </section>
  );
}
