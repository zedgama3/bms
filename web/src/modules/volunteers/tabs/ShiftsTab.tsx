import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";
import { useReadOnly, useSession } from "@/api";
import { useT } from "@/i18n";
import { Button, controlClass, EmptyState, Field, linkButtonClass, Loading, useToast } from "@/ui";
import { vol } from "../calls";
import { FormDialog } from "../FormDialog";
import { holdsAt, omitEmpty, text } from "../helpers";
import { P } from "../permissions";
import { ShowMore, usePaged } from "../paging";
import { nameIn, useEvents } from "../queries";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { ScreenError } from "../ScreenError";
import { ShiftWhen } from "../ShiftWhen";
import type { ShiftSummary } from "../types";

/** A department's shifts by event with fill counts, **New shift** and **Copy from a previous
 *  event** (`shifts.manage`). */
export function ShiftsTab({ department }: { department: string }) {
  const t = useT();
  const toast = useToast();
  const client = useQueryClient();
  const session = useSession();
  const { readOnly } = useReadOnly();
  const canManage = holdsAt(session, P.shiftsManage, department);
  const [event, setEvent] = useState("");
  const [copying, setCopying] = useState(false);
  const events = useEvents();
  const {
    q,
    rows: list,
    error,
  } = usePaged<ShiftSummary>(
    ["volunteers", "shifts", { department, event, managed: canManage }],
    (after, signal) =>
      vol.shifts(
        { department, ...(event ? { event } : {}), ...(canManage ? { canceled: true } : {}) },
        after,
        { signal },
      ),
  );

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap gap-3">
        {canManage ? (
          readOnly ? (
            <Button disabled aria-describedby="shiftstab-readonly">
              {t("module.volunteers.tab.shifts.new")}
            </Button>
          ) : (
            <Link
              to={`/volunteers/departments/${department}/shifts/new`}
              className={linkButtonClass("primary")}
            >
              {t("module.volunteers.tab.shifts.new")}
            </Link>
          )
        ) : null}
        {canManage ? (
          <Button
            variant="secondary"
            disabled={readOnly}
            {...(readOnly ? { "aria-describedby": "shiftstab-readonly" } : {})}
            onClick={() => setCopying(true)}
          >
            {t("module.volunteers.tab.shifts.copy")}
          </Button>
        ) : null}
      </div>
      <ReadOnlyNote id="shiftstab-readonly" readOnly={readOnly} />
      <Field label={t("module.volunteers.filter.event")} name="event">
        {(control) => (
          <select
            {...control}
            className={controlClass}
            value={event}
            onChange={(e) => setEvent(e.target.value)}
          >
            <option value="">{t("module.volunteers.filter.any_event")}</option>
            {(events.data ?? []).map((ev) => (
              <option key={ev.id} value={ev.id}>
                {ev.name}
              </option>
            ))}
          </select>
        )}
      </Field>
      {q.isPending ? <Loading /> : null}
      {error ? <ScreenError error={error} onRetry={() => void q.refetch()} /> : null}
      {q.data && list.length === 0 ? (
        <EmptyState>
          {t(
            canManage
              ? "module.volunteers.tab.shifts.empty_manage"
              : "module.volunteers.tab.shifts.empty",
          )}
        </EmptyState>
      ) : null}
      {list.length > 0 ? (
        <ul className="flex flex-col gap-3">
          {list.map((s) => (
            <li
              key={s.id}
              className="flex flex-col gap-1 rounded-md border border-border bg-surface p-3"
            >
              <h3 className="text-lg font-bold">
                <Link
                  to={`/volunteers/shifts/${s.id}`}
                  className="inline-flex min-h-11 items-center underline"
                >
                  {s.label}
                </Link>
              </h3>
              <p className="text-muted">
                {s.position_name ?? ""}
                {s.event ? ` · ${nameIn(events.data, s.event)}` : ""}
              </p>
              <p>
                <ShiftWhen shift={s} />
              </p>
              <p className="font-semibold">
                {s.canceled_at
                  ? t("module.volunteers.canceled")
                  : t("module.volunteers.tab.shifts.fill", {
                      booked: s.booked ?? 0,
                      slots: s.slots,
                    })}
              </p>
            </li>
          ))}
        </ul>
      ) : null}
      <ShowMore q={q} />

      <FormDialog
        open={copying}
        title={t("module.volunteers.copy.title")}
        submit={t("module.volunteers.copy.submit")}
        onClose={() => setCopying(false)}
        onSubmit={async (data) => {
          const res = await vol.copyShifts(
            department,
            omitEmpty({ from: text(data.get("from")), to: text(data.get("to")) }),
          );
          const n = res?.created ?? 0;
          toast.show(t("module.volunteers.copy.done", { count: n }));
          await client.invalidateQueries({ queryKey: ["volunteers"] });
        }}
      >
        {(errors) => (
          <>
            <p className="text-sm text-muted">{t("module.volunteers.copy.hint")}</p>
            {(["from", "to"] as const).map((name) => (
              <Field
                key={name}
                label={t(`module.volunteers.copy.${name}`)}
                name={name}
                required
                error={errors[name]}
              >
                {(control) => (
                  <select {...control} className={controlClass} defaultValue="">
                    <option value="" disabled>
                      {t("module.volunteers.choose")}
                    </option>
                    {(events.data ?? []).map((ev) => (
                      <option key={ev.id} value={ev.id}>
                        {ev.name}
                      </option>
                    ))}
                  </select>
                )}
              </Field>
            ))}
          </>
        )}
      </FormDialog>
    </div>
  );
}
