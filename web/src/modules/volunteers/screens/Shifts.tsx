import { useState } from "react";
import { Link, useSearchParams } from "react-router";
import { useFormat, useT } from "@/i18n";
import { Button, controlClass, DateField, EmptyState, Field, linkButtonClass, Loading } from "@/ui";
import { vol } from "../calls";
import { ShowMore, usePaged } from "../paging";
import { nameIn, useDepartments, useEvents, useQualifications } from "../queries";
import { ScreenError } from "../ScreenError";
import { ShiftWhen } from "../ShiftWhen";
import { SignUpControl } from "../SignUpControl";
import { browserZone, dayKey, dayWindow } from "../time";
import type { ShiftSummary } from "../types";

/** `/volunteers/`: the shifts the person may see, filtered by event, department and day, each
 *  with Sign up, Booked or the reason they cannot. Phone first: one card per shift. */
export function Shifts() {
  const t = useT();
  const format = useFormat();
  const [params, setParams] = useSearchParams();
  const event = params.get("event") ?? "";
  const department = params.get("department") ?? "";
  const day = params.get("day") ?? "";

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
    ["volunteers", "shifts", { event, department, from: range?.from, to: range?.to }],
    (after, signal) =>
      vol.shifts(
        {
          ...(event ? { event } : {}),
          ...(department ? { department } : {}),
          ...(range ?? {}),
        },
        after,
        { signal },
      ),
  );
  // Phones start with the filters folded away so the first shift is on screen.
  const [open, setOpen] = useState(event !== "" || department !== "" || day !== "");
  const quals = useQualifications();

  const visible = day ? all.filter((s) => dayKey(s.starts, s.zone) === day) : all;
  const byDay = new Map<string, ShiftSummary[]>();
  for (const s of visible) {
    const k = dayKey(s.starts, s.zone);
    byDay.set(k, [...(byDay.get(k) ?? []), s]);
  }
  const filtered = event !== "" || department !== "" || day !== "";
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
          <ul className="flex flex-col gap-3">
            {list.map((s) => {
              const free = Math.max(0, s.slots - (s.booked ?? 0));
              const deptName = s.department_name || nameIn(departments.data, s.department);
              const pos = s.position_name ?? "";
              return (
                <li
                  key={s.id}
                  className="flex flex-col gap-2 rounded-md border border-border bg-surface p-3"
                >
                  <h3 className="text-lg font-bold">
                    <Link
                      to={`/volunteers/shifts/${s.id}`}
                      className="inline-flex min-h-11 items-center underline"
                    >
                      {s.label}
                    </Link>
                  </h3>
                  <p className="text-muted">{[deptName, pos].filter(Boolean).join(" · ")}</p>
                  <p>
                    <ShiftWhen shift={s} />
                  </p>
                  {s.location ? <p>{s.location}</p> : null}
                  <p className="font-semibold">
                    {s.canceled_at
                      ? t("module.volunteers.canceled")
                      : free === 0
                        ? t("module.volunteers.full")
                        : t("module.volunteers.free", { free, slots: s.slots })}
                  </p>
                  <SignUpControl shift={s} qualifications={quals.data} />
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
