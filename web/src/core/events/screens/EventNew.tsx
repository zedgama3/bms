import { useRef } from "react";
import type { FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router";
import { api, ApiError, useAction, useReadOnly, useSession } from "@/api";
import { useT } from "@/i18n";
import { Button, ErrorSummary, Loading, useFieldErrors, useToast } from "@/ui";
import { EventFields, readEvent } from "../EventFields";
import { Forbidden } from "../Forbidden";
import { holdsSite } from "../helpers";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { ScreenError } from "../ScreenError";
import { useKindLabel } from "../useKindLabel";

/** `/events/new` (`events.manage`): name, kind, dates in the chosen zone, and which event to copy
 *  the modules' set-up from. The event page opens with the set-up outcome. */
export function EventNew() {
  const t = useT();
  const toast = useToast();
  const navigate = useNavigate();
  const client = useQueryClient();
  const session = useSession();
  const { readOnly } = useReadOnly();
  const formRef = useRef<HTMLFormElement>(null);
  const act = useAction();
  const fieldErrors = useFieldErrors(act.error);
  const allowed = holdsSite(session, "events.manage");
  const kindsQ = useQuery({
    queryKey: ["events", "kinds"],
    enabled: allowed,
    queryFn: ({ signal }) => api.GET("/api/events/kinds", { signal }),
  });
  const eventsQ = useQuery({
    queryKey: ["events", "list"],
    enabled: allowed,
    queryFn: ({ signal }) => api.GET("/api/events/", { signal }),
  });
  const kinds = kindsQ.data?.kinds ?? [];
  const kindLabel = useKindLabel(kinds);
  const loadError =
    (kindsQ.error instanceof ApiError ? kindsQ.error : null) ??
    (eventsQ.error instanceof ApiError ? eventsQ.error : null);

  if (!allowed) return <Forbidden title={t("core.events.new.title")} />;

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const v = readEvent(new FormData(e.currentTarget));
    void act.run(async () => {
      const created = await api.POST("/api/events/", {
        body: {
          name: v.name,
          kind: v.kind,
          dates: v.dates,
          zone: v.zone,
          ...(v.copyFrom ? { copy_from: v.copyFrom } : {}),
        },
      });
      void client.invalidateQueries({ queryKey: ["events"] });
      toast.show(t("core.events.new.done"));
      void navigate(`/events/${created.event.id}`, {
        replace: true,
        state: { setup: created.setup },
      });
    });
  };

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-2xl font-bold">{t("core.events.new.title")}</h1>
      <div>
        <Link to="/events" className="inline-flex min-h-11 items-center underline">
          {t("core.events.new.back")}
        </Link>
      </div>
      {kindsQ.isPending || eventsQ.isPending ? <Loading /> : null}
      {loadError ? (
        <ScreenError
          error={loadError}
          onRetry={() => {
            void kindsQ.refetch();
            void eventsQ.refetch();
          }}
        />
      ) : null}
      {kindsQ.data && eventsQ.data ? (
        <form ref={formRef} onSubmit={submit} className="flex flex-col gap-5" noValidate>
          <ErrorSummary error={act.error} formRef={formRef} />
          <EventFields
            kinds={kinds}
            kindLabel={kindLabel}
            events={eventsQ.data.events}
            errors={fieldErrors}
          />
          <ReadOnlyNote id="event-new-readonly" readOnly={readOnly} />
          <div>
            <Button
              type="submit"
              busy={act.busy}
              disabled={readOnly}
              {...(readOnly ? { "aria-describedby": "event-new-readonly" } : {})}
            >
              {t("core.events.new.submit")}
            </Button>
          </div>
        </form>
      ) : null}
    </div>
  );
}
