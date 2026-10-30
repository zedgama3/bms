import { useState } from "react";
import { Link, useLocation, useSearchParams } from "react-router";
import { useFormat, useT } from "@/i18n";
import {
  Button,
  Check,
  controlClass,
  DateField,
  EmptyState,
  Field,
  linkButtonClass,
  Loading,
} from "@/ui";
import { backTo } from "../back";
import { vol } from "../calls";
import { ShowMore, usePaged } from "../paging";
import { nameIn, useDepartments, useEvents, useQualifications } from "../queries";
import { ScreenError } from "../ScreenError";
import { ShiftTimes } from "../ShiftTimes";
import { SignUpControl } from "../SignUpControl";
import { browserZone, dayKey, dayWindow } from "../time";
import type { ShiftSummary } from "../types";

/** `/volunteers/`: the shifts the person may see, filtered by event, department, day and
 *  "only what I can take", each with Sign up, Booked or the short reason they cannot. Phone
 *  first: dense rows grouped by day, three short lines on a phone and one grid row from 640 px. */
export function Shifts() {
  const t = useT();
  const format = useFormat();
  const [params, setParams] = useSearchParams();
  const event = params.get("event") ?? "";
  const department = params.get("department") ?? "";
  const day = params.get("day") ?? "";
  const eligible = params.get("eligible") === "1";
  const location = useLocation();

  const departments = useDepartments();
  const events = useEvents();
  // The day is asked of the server (a window around it in the event's zone, else the browser's);
  // the exact day, in each shift's own zone, is kept below.
  const zone = events.data?.find((e) => e.id === event)?.zone ?? browserZone();
  const range = day ? dayWindow(day, zone) : null;
  const {
    q: shifts,
    rows: all,
    error,
  } = usePaged<ShiftSummary>(
    ["volunteers", "shifts", { event, department, from: range?.from, to: range?.to, eligible }],
    (after, signal) =>
      vol.shifts(
        {
          ...(event ? { event } : {}),
          ...(department ? { department } : {}),
          ...(range ?? {}),
          ...(eligible ? { eligible: true } : {}),
        },
        after,
        { signal },
      ),
  );
  // Phones start with the filters folded away so the first shift is on screen.
  const [open, setOpen] = useState(event !== "" || department !== "" || day !== "" || eligible);
  const quals = useQualifications();

  const visible = day ? all.filter((s) => dayKey(s.starts, s.zone) === day) : all;
  const byDay = new Map<string, ShiftSummary[]>();
  for (const s of visible) {
    const k = dayKey(s.starts, s.zone);
    byDay.set(k, [...(byDay.get(k) ?? []), s]);
  }
  const filtered = event !== "" || department !== "" || day !== "" || eligible;
  const set = (name: string, value: string) =>
    setParams(
      (p) => {
        const next = new URLSearchParams(p);
        if (value) next.set(name, value);
        else next.delete(name);
        return next;
      },
      { replace: true },
    );

  return (
    <div className="flex flex-col gap-4" aria-busy={shifts.isPending || undefined}>
      <h1 className="text-2xl font-bold">{t("module.volunteers.shifts.title")}</h1>
      <div className="sm:hidden">
        <Button
          variant="secondary"
          aria-expanded={open}
          aria-controls="shift-filters"
          onClick={() => setOpen(!open)}
        >
          {t("module.volunteers.shifts.filters_toggle")}
        </Button>
      </div>
      <form
        id="shift-filters"
        role="search"
        aria-label={t("module.volunteers.shifts.filters")}
        className={`${open ? "flex" : "hidden"} flex-col gap-3 sm:grid sm:grid-cols-3`}
        onSubmit={(e) => e.preventDefault()}
      >
        <Field label={t("module.volunteers.filter.event")} name="event">
          {(control) => (
            <select
              {...control}
              className={controlClass}
              value={event}
              onChange={(e) => set("event", e.target.value)}
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
        <Field label={t("module.volunteers.filter.department")} name="department">
          {(control) => (
            <select
              {...control}
              className={controlClass}
              value={department}
              onChange={(e) => set("department", e.target.value)}
            >
              <option value="">{t("module.volunteers.filter.any_department")}</option>
              {(departments.data ?? []).map((d) => (
                <option key={d.id} value={d.id}>
                  {d.name}
                </option>
              ))}
            </select>
          )}
        </Field>
        <DateField
          label={t("module.volunteers.filter.day")}
          name="day"
          value={day}
          onChange={(e) => set("day", e.target.value)}
        />
        <div className="sm:col-span-3">
          <Check
            name="eligible"
            checked={eligible}
            onChange={(e) => set("eligible", e.target.checked ? "1" : "")}
            label={t("module.volunteers.filter.eligible")}
          />
        </div>
        {filtered ? (
          <div className="sm:col-span-3">
            <Button variant="secondary" onClick={() => setParams({}, { replace: true })}>
              {t("module.volunteers.filter.clear")}
            </Button>
          </div>
        ) : null}
      </form>

      {shifts.isPending ? <Loading /> : null}
      {error ? <ScreenError error={error} onRetry={() => void shifts.refetch()} /> : null}
      {shifts.data && visible.length === 0 && !shifts.hasNextPage ? (
        <EmptyState
          {...(filtered
            ? {
                action: (
                  <Button variant="secondary" onClick={() => setParams({}, { replace: true })}>
                    {t("module.volunteers.filter.clear")}
                  </Button>
                ),
              }
            : {})}
        >
          {t("module.volunteers.shifts.empty")}
        </EmptyState>
      ) : null}

      {[...byDay.entries()].map(([k, list]) => (
        <section key={k} aria-labelledby={`day-${k}`} className="flex flex-col gap-3">
          <h2 id={`day-${k}`} className="text-xl font-bold">
            {format.date(list[0]?.starts ?? "", list[0]?.zone)}
          </h2>
          <ul className="flex flex-col divide-y divide-border border-y border-border">
            {list.map((s) => {
              const deptName = s.department_name || nameIn(departments.data, s.department);
              const details = [deptName, s.position_name ?? "", s.location ?? ""]
                .filter(Boolean)
                .join(" · ");
              return (
                <li
                  key={s.id}
                  className="flex flex-col gap-1 py-2 md:grid md:grid-cols-[11rem_minmax(0,1fr)_minmax(0,1.2fr)_8rem_11rem] md:items-center md:gap-x-3"
                >
                  <div className="flex flex-wrap items-center gap-x-3 md:contents">
                    <p className="font-semibold">
                      <ShiftTimes shift={s} />
                    </p>
                    <h3 className="text-lg font-bold">
                      <Link
                        to={`/volunteers/shifts/${s.id}`}
                        state={backTo("shifts", location)}
                        className="inline-flex min-h-11 items-center underline"
                      >
                        {s.label}
                      </Link>
                    </h3>
                  </div>
                  <div className="flex flex-wrap gap-x-3 md:contents">
                    <p className="min-w-0 text-muted">{details}</p>
                    <p className="font-semibold">
                      {s.canceled_at
                        ? t("module.volunteers.canceled")
                        : t("module.volunteers.fill", { booked: s.booked ?? 0, slots: s.slots })}
                    </p>
                  </div>
                  {s.canceled_at ? null : (
                    <div>
                      <SignUpControl shift={s} qualifications={quals.data} short />
                    </div>
                  )}
                </li>
              );
            })}
          </ul>
        </section>
      ))}
      <ShowMore q={shifts} />
      <div>
        <Link to="/volunteers/me" className={linkButtonClass("secondary")}>
          {t("module.volunteers.me.title")}
        </Link>
      </div>
    </div>
  );
}
