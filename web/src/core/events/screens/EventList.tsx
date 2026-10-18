import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";
import { api, ApiError, useReadOnly, useSession } from "@/api";
import { useFormat, useT } from "@/i18n";
import { Button, EmptyState, linkButtonClass, Loading } from "@/ui";
import { holdsSite } from "../helpers";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { ScreenError } from "../ScreenError";
import type { EventKind, EventRecord } from "../types";
import { useKindLabel } from "../useKindLabel";

type Group = "now" | "upcoming" | "past";

/** Which group an event belongs to, from its dates and its closed flag, at an instant. */
function groupOf(e: EventRecord, now: number): Group {
  if (e.closed || Date.parse(e.dates.end) <= now) return "past";
  return Date.parse(e.dates.start) > now ? "upcoming" : "now";
}

/** `/events`: Now, Upcoming and Past, computed in the browser. Dates are shown in the event's own
 *  zone (the zone is added when it differs from the browser's). */
export function EventList() {
  const t = useT();
  const format = useFormat();
  const session = useSession();
  const { readOnly } = useReadOnly();
  const canManage = holdsSite(session, "events.manage");
  const q = useQuery({
    queryKey: ["events", "list"],
    queryFn: ({ signal }) => api.GET("/api/events/", { signal }),
  });
  // Kinds only label the cards: a failure leaves the keys, it does not fail the screen.
  const kindsQ = useQuery({
    queryKey: ["events", "kinds"],
    queryFn: ({ signal }) => api.GET("/api/events/kinds", { signal }),
  });
  const kinds: readonly EventKind[] = kindsQ.data?.kinds ?? [];
  const kindLabel = useKindLabel(kinds);
  // The groups are as of when the screen opened.
  const [now] = useState(() => Date.now());
  const error = q.error instanceof ApiError ? q.error : null;
  const events = q.data?.events ?? [];
  const groups: Record<Group, EventRecord[]> = { now: [], upcoming: [], past: [] };
  for (const e of events) groups[groupOf(e, now)].push(e);
  groups.upcoming.sort((a, b) => Date.parse(a.dates.start) - Date.parse(b.dates.start));
  const order: Group[] = ["now", "upcoming", "past"];

  return (
    <div className="flex flex-col gap-4" aria-busy={q.isPending || undefined}>
      <h1 className="text-2xl font-bold">{t("core.events.list.title")}</h1>
      {canManage ? (
        readOnly ? (
          <div className="flex flex-col gap-1">
            <div>
              <Button disabled aria-describedby="events-readonly">
                {t("core.events.list.new")}
              </Button>
            </div>
            <ReadOnlyNote id="events-readonly" readOnly />
          </div>
        ) : (
          <div>
            <Link to="/events/new" className={linkButtonClass("primary")}>
              {t("core.events.list.new")}
            </Link>
          </div>
        )
      ) : null}
      {q.isPending ? <Loading /> : null}
      {error ? <ScreenError error={error} onRetry={() => void q.refetch()} /> : null}
      {q.data && events.length === 0 ? (
        <EmptyState>{t("core.events.list.empty")}</EmptyState>
      ) : null}
      {order.map((g) =>
        groups[g].length === 0 ? null : (
          <section key={g} aria-labelledby={`events-${g}`} className="flex flex-col gap-3">
            <h2 id={`events-${g}`} className="text-xl font-bold">
              {t(`core.events.group.${g}`)}
            </h2>
            <ul className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {groups[g].map((e) => (
                <li key={e.id} className="rounded-md border border-border bg-surface p-4">
                  <h3 className="text-lg font-bold">
                    <Link
                      to={`/events/${e.id}`}
                      className="inline-flex min-h-11 items-center break-words underline"
                    >
                      {e.name}
                    </Link>
                  </h3>
                  <p className="text-muted">{kindLabel(e.kind)}</p>
                  <p className="mt-1">
                    {t("core.events.dates", {
                      start: format.dateTime(e.dates.start, e.zone),
                      end: format.dateTime(e.dates.end, e.zone),
                    })}
                  </p>
                  {e.closed ? (
                    <p className="mt-1">
                      <span className="rounded-md border border-border px-2 py-1 text-sm font-semibold">
                        {t("core.events.closed_badge")}
                      </span>
                    </p>
                  ) : null}
                </li>
              ))}
            </ul>
          </section>
        ),
      )}
    </div>
  );
}
