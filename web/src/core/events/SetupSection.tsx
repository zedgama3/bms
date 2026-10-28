import { useRef, useState } from "react";
import type { FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, useAction } from "@/api";
import { isMessageKey, useT } from "@/i18n";
import {
  Button,
  Dialog,
  ErrorSummary,
  Field,
  Loading,
  controlClass,
  useFieldErrors,
  useToast,
} from "@/ui";
import { focusSoon } from "./focus";
import { ReadOnlyNote } from "./ReadOnlyNote";
import { ScreenError } from "./ScreenError";
import { text } from "./text";
import type { SetupResult } from "./types";

/** What the new-event screen passed in the navigation state: the set-up outcome, if it has the
 *  shape of one. Anything else is ignored. */
export function parseSetup(state: unknown): SetupResult[] | null {
  if (typeof state !== "object" || state === null) return null;
  const raw = (state as { setup?: unknown }).setup;
  if (!Array.isArray(raw)) return null;
  const out: SetupResult[] = [];
  for (const r of raw as unknown[]) {
    if (typeof r !== "object" || r === null) continue;
    const o = r as Record<string, unknown>;
    if (typeof o["module"] !== "string") continue;
    out.push({
      module: o["module"],
      failed: o["failed"] === true,
      items: typeof o["items"] === "number" ? o["items"] : 0,
      ...(typeof o["code"] === "string" ? { code: o["code"] } : {}),
    });
  }
  return out;
}

/** The Set-up card: each module's result from creating the event or from the last "Run set-up
 *  again", and the button to run it again (an open event, `events.manage`). */
export function SetupSection({
  eventId,
  canRun,
  readOnly,
  initial,
}: {
  eventId: string;
  canRun: boolean;
  readOnly: boolean;
  initial: SetupResult[] | null;
}) {
  const t = useT();
  const toast = useToast();
  const client = useQueryClient();
  const [results, setResults] = useState<SetupResult[] | null>(initial);
  const [open, setOpen] = useState(false);
  const formRef = useRef<HTMLFormElement>(null);
  const act = useAction();
  const fieldErrors = useFieldErrors(act.error);
  const eventsQ = useQuery({
    queryKey: ["events", "list"],
    enabled: canRun && open,
    queryFn: ({ signal }) => api.GET("/api/events/", { signal }),
  });
  const loadError = eventsQ.error instanceof ApiError ? eventsQ.error : null;
  const sources = (eventsQ.data?.events ?? []).filter((e) => e.id !== eventId);

  const run = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const from = text(new FormData(e.currentTarget).get("copy_from"));
    void act.run(async () => {
      const out = await api.POST("/api/events/{event}/setup", {
        params: { path: { event: eventId } },
        body: { copy_from: from },
      });
      setResults(out.setup);
      void client.invalidateQueries({ queryKey: ["events"] });
      toast.show(t("core.events.setup.done"));
      setOpen(false);
      focusSoon("#event-setup");
    });
  };

  if (results === null && !canRun) return null;
  const moduleTitle = (m: string) => {
    const key = `module.${m}.title`;
    return isMessageKey(key) ? t(key) : m;
  };

  return (
    <section
      aria-labelledby="event-setup"
      className="flex flex-col gap-3 rounded-md border border-border bg-surface p-4"
    >
      <h2 id="event-setup" className="text-lg font-bold">
        {t("core.events.setup.title")}
      </h2>
      {results === null ? <p className="text-muted">{t("core.events.setup.not_run")}</p> : null}
      {results !== null && results.length === 0 ? <p>{t("core.events.setup.nothing")}</p> : null}
      {results !== null && results.length > 0 ? (
        <ul className="flex flex-col gap-1">
          {results.map((r) => (
            <li key={r.module} className="break-words">
              {r.failed
                ? t("core.events.setup.failed", {
                    module: moduleTitle(r.module),
                    code: r.code ?? "",
                  })
                : r.items === 0
                  ? t("core.events.setup.none", { module: moduleTitle(r.module) })
                  : t("core.events.setup.copied", {
                      module: moduleTitle(r.module),
                      count: r.items,
                    })}
            </li>
          ))}
        </ul>
      ) : null}
      {canRun ? (
        <div className="flex flex-col gap-1">
          <div>
            <Button
              variant="secondary"
              disabled={readOnly}
              {...(readOnly ? { "aria-describedby": "setup-readonly" } : {})}
              onClick={() => {
                act.clear();
                setOpen(true);
              }}
            >
              {t("core.events.setup.run")}
            </Button>
          </div>
          <ReadOnlyNote id="setup-readonly" readOnly={readOnly} />
        </div>
      ) : null}

      <Dialog open={open} title={t("core.events.setup.run")} onClose={() => setOpen(false)}>
        {eventsQ.isPending ? <Loading /> : null}
        {loadError ? (
          <ScreenError error={loadError} onRetry={() => void eventsQ.refetch()} />
        ) : null}
        {eventsQ.data ? (
          <form ref={formRef} onSubmit={run} className="flex flex-col gap-4" noValidate>
            {open ? <ErrorSummary error={act.error} formRef={formRef} /> : null}
            {sources.length === 0 ? <p>{t("core.events.setup.no_sources")}</p> : null}
            <Field
              label={t("core.events.field.copy_from")}
              name="copy_from"
              required
              hint={t("core.events.setup.run_hint")}
              error={fieldErrors["copy_from"]}
            >
              {(control) => (
                <select {...control} className={controlClass} defaultValue="">
                  <option value="" disabled>
                    {t("core.events.setup.choose")}
                  </option>
                  {sources.map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.name}
                    </option>
                  ))}
                </select>
              )}
            </Field>
            <div className="flex flex-wrap gap-3">
              <Button type="submit" busy={act.busy}>
                {t("core.events.setup.run_confirm")}
              </Button>
              <Button variant="secondary" onClick={() => setOpen(false)}>
                {t("core.events.cancel")}
              </Button>
            </div>
          </form>
        ) : null}
      </Dialog>
    </section>
  );
}
