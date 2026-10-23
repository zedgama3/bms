import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";
import { useReadOnly } from "@/api";
import { isMessageKey, useT } from "@/i18n";
import { Button, EmptyState, linkButtonClass, Loading, useToast } from "@/ui";
import { vol } from "../calls";
import { ConfirmDialog } from "../ConfirmDialog";
import { ShowMore, usePaged } from "../paging";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { ScreenError } from "../ScreenError";
import { ShiftWhen } from "../ShiftWhen";
import type { ScheduleEntry } from "../types";

/** My bookings, upcoming by default, past ones on request; each upcoming booking can be canceled. */
export function MySchedule() {
  const t = useT();
  const toast = useToast();
  const client = useQueryClient();
  const { readOnly } = useReadOnly();
  const [past, setPast] = useState(false);
  const [toggled, setToggled] = useState(false);
  const [target, setTarget] = useState<ScheduleEntry | null>(null);
  const { q, rows, error } = usePaged<ScheduleEntry>(
    ["volunteers", "schedule", past],
    (after, signal) => vol.schedule(past, after, { signal }),
  );
  // Upcoming also lists bookings someone else canceled (struck through, with the reason) until
  // the shift passes; the person's own cancellations never come back. Past lists booked rows.
  const entries = past ? rows.filter((e) => e.status !== "canceled") : rows;
  const [now] = useState(() => Date.now());

  return (
    <section aria-labelledby="me-schedule" className="flex flex-col gap-3">
      <h2 id="me-schedule" className="text-xl font-bold">
        {t("module.volunteers.me.schedule")}
      </h2>
      <div
        role="group"
        aria-label={t("module.volunteers.me.tabs")}
        className="flex flex-wrap gap-2"
      >
        {([false, true] as const).map((isPast) => (
          <Button
            key={String(isPast)}
            variant={past === isPast ? "primary" : "secondary"}
            aria-pressed={past === isPast}
            onClick={() => {
              setPast(isPast);
              setToggled(true);
            }}
          >
            {t(isPast ? "module.volunteers.me.tab_past" : "module.volunteers.me.tab_upcoming")}
          </Button>
        ))}
      </div>
      <p role="status" className="sr-only">
        {toggled && q.data && !q.isFetching
          ? t(past ? "module.volunteers.me.showing_past" : "module.volunteers.me.showing_upcoming")
          : ""}
      </p>
      {q.isPending ? <Loading /> : null}
      {error ? <ScreenError error={error} onRetry={() => void q.refetch()} /> : null}
      {q.data && entries.length === 0 && !q.hasNextPage ? (
        <EmptyState
          {...(past
            ? {}
            : {
                action: (
                  <Link to="/volunteers" className={linkButtonClass("primary")}>
                    {t("module.volunteers.me.find_shifts")}
                  </Link>
                ),
              })}
        >
          {t(past ? "module.volunteers.me.no_past" : "module.volunteers.me.no_shifts")}
        </EmptyState>
      ) : null}
      {entries.length > 0 ? (
        <ul className="flex flex-col gap-3">
          {entries.map((e) => {
            const removed = e.status === "canceled";
            const reasonKey = `module.volunteers.me.removed.${e.cancel_reason ?? ""}`;
            return (
              <li
                key={e.id}
                className="flex flex-col gap-1 rounded-md border border-border bg-surface p-3"
              >
                <h3 className={`text-lg font-bold ${removed ? "line-through" : ""}`}>
                  <Link
                    to={`/volunteers/shifts/${e.shift.id}`}
                    className="inline-flex min-h-11 items-center underline"
                  >
                    {e.shift.label}
                  </Link>
                </h3>
                {removed ? (
                  <p className="font-semibold">
                    {isMessageKey(reasonKey)
                      ? t(reasonKey)
                      : t("module.volunteers.me.removed.unknown")}
                  </p>
                ) : null}
                <p className="text-muted">
                  {[e.department_name, e.position_name].filter(Boolean).join(" · ")}
                </p>
                <p>
                  <ShiftWhen shift={e.shift} />
                </p>
                {!removed && Date.parse(e.shift.starts) > now && !e.shift.canceled_at ? (
                  <div className="flex flex-col gap-1">
                    <div>
                      <Button
                        variant="secondary"
                        disabled={readOnly}
                        {...(readOnly ? { "aria-describedby": "schedule-readonly" } : {})}
                        onClick={() => setTarget(e)}
                      >
                        {t("module.volunteers.me.cancel")}
                        <span className="sr-only"> {e.shift.label}</span>
                      </Button>
                    </div>
                  </div>
                ) : e.shift.canceled_at && !removed ? (
                  <p className="font-semibold">{t("module.volunteers.canceled")}</p>
                ) : null}
              </li>
            );
          })}
        </ul>
      ) : null}
      <ShowMore q={q} />
      <ReadOnlyNote id="schedule-readonly" readOnly={readOnly} />
      <ConfirmDialog
        open={target !== null}
        title={t("module.volunteers.shift.cancel_mine_title")}
        body={t("module.volunteers.shift.cancel_mine_body", { label: target?.shift.label ?? "" })}
        confirm={t("module.volunteers.shift.cancel_mine_confirm")}
        returnFocus="#me-schedule"
        onClose={() => setTarget(null)}
        onConfirm={async () => {
          if (!target) return;
          await vol.cancelSignup(target.id);
          toast.show(t("module.volunteers.booking_canceled"));
          await client.invalidateQueries({ queryKey: ["volunteers"] });
        }}
      />
    </section>
  );
}
