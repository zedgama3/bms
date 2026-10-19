import { useRef, useState } from "react";
import type { FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";
import { api, ApiError, useReadOnly, useSession } from "@/api";
import { useT } from "@/i18n";
import {
  Button,
  Dialog,
  EmptyState,
  ErrorSummary,
  controlClass,
  Field,
  Loading,
  useFieldErrors,
  useToast,
} from "@/ui";
import { Forbidden } from "../Forbidden";
import { focusSoon } from "../focus";
import { holdsSite } from "../helpers";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { ScreenError } from "../ScreenError";
import { text } from "../text";
import type { EventKind } from "../types";
import { useAction } from "../useAction";
import { useKindTitle } from "../useKindLabel";

/** `/events/kinds` (`events.manage`): the kinds as cards, a new kind, and rename in place. A kind
 *  is never deleted. */
export function EventKinds() {
  const t = useT();
  const toast = useToast();
  const client = useQueryClient();
  const session = useSession();
  const { readOnly } = useReadOnly();
  const allowed = holdsSite(session, "events.manage");
  const [adding, setAdding] = useState(false);
  const [renaming, setRenaming] = useState("");
  const addRef = useRef<HTMLFormElement>(null);
  const addAct = useAction();
  const addErrors = useFieldErrors(addAct.error);
  const q = useQuery({
    queryKey: ["events", "kinds"],
    enabled: allowed,
    queryFn: ({ signal }) => api.GET("/api/events/kinds", { signal }),
  });
  const kinds = q.data?.kinds ?? [];
  const title = useKindTitle();
  const error = q.error instanceof ApiError ? q.error : null;

  if (!allowed) return <Forbidden title={t("core.events.kinds.title")} />;

  const add = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const data = new FormData(e.currentTarget);
    void addAct.run(async () => {
      await api.POST("/api/events/kinds", {
        body: { key: text(data.get("key")).trim(), name: text(data.get("name")).trim() },
      });
      void client.invalidateQueries({ queryKey: ["events"] });
      toast.show(t("core.events.kinds.added"));
      setAdding(false);
      focusSoon("main h1");
    });
  };

  return (
    <div className="flex flex-col gap-4" aria-busy={q.isPending || undefined}>
      <h1 className="text-2xl font-bold">{t("core.events.kinds.title")}</h1>
      <p className="text-muted">{t("core.events.kinds.intro")}</p>
      <div className="flex flex-col gap-1">
        <div>
          <Button
            disabled={readOnly}
            {...(readOnly ? { "aria-describedby": "kinds-readonly" } : {})}
            onClick={() => {
              addAct.clear();
              setAdding(true);
            }}
          >
            {t("core.events.kinds.new")}
          </Button>
        </div>
        <ReadOnlyNote id="kinds-readonly" readOnly={readOnly} />
      </div>
      {q.isPending ? <Loading /> : null}
      {error ? <ScreenError error={error} onRetry={() => void q.refetch()} /> : null}
      {q.data && kinds.length === 0 ? (
        <EmptyState>{t("core.events.kinds.empty")}</EmptyState>
      ) : null}
      {kinds.length > 0 ? (
        <ul className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {kinds.map((k) => (
            <li key={k.key} className="rounded-md border border-border bg-surface p-4">
              {renaming === k.key ? (
                <RenameKind
                  kind={k}
                  onDone={() => {
                    setRenaming("");
                    focusSoon(`#kind-rename-${k.key}`);
                  }}
                  onCancel={() => {
                    setRenaming("");
                    focusSoon(`#kind-rename-${k.key}`);
                  }}
                />
              ) : (
                <>
                  <h2 className="break-words text-lg font-bold">{title(k)}</h2>
                  <p className="text-muted">
                    <code>{k.key}</code>
                  </p>
                  <div className="mt-2">
                    <Button
                      id={`kind-rename-${k.key}`}
                      variant="secondary"
                      disabled={readOnly}
                      {...(readOnly ? { "aria-describedby": "kinds-readonly" } : {})}
                      onClick={() => setRenaming(k.key)}
                    >
                      {t("core.events.kinds.rename")} <span className="sr-only">{title(k)}</span>
                    </Button>
                  </div>
                </>
              )}
            </li>
          ))}
        </ul>
      ) : null}
      <div>
        <Link to="/events" className="inline-flex min-h-11 items-center underline">
          {t("core.events.kinds.back")}
        </Link>
      </div>

      <Dialog open={adding} title={t("core.events.kinds.new")} onClose={() => setAdding(false)}>
        <form ref={addRef} onSubmit={add} className="flex flex-col gap-4" noValidate>
          {adding ? <ErrorSummary error={addAct.error} formRef={addRef} /> : null}
          <Field
            label={t("core.events.kinds.key")}
            name="key"
            hint={t("core.events.kinds.key_hint")}
            required
            error={addErrors["key"]}
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
              />
            )}
          </Field>
          <Field
            label={t("core.events.kinds.name")}
            name="name"
            required
            maxLength={60}
            autoComplete="off"
            error={addErrors["name"]}
          />
          <div className="flex flex-wrap gap-3">
            <Button type="submit" busy={addAct.busy}>
              {t("core.events.kinds.add")}
            </Button>
            <Button variant="secondary" onClick={() => setAdding(false)}>
              {t("core.events.cancel")}
            </Button>
          </div>
        </form>
      </Dialog>
    </div>
  );
}

function RenameKind({
  kind,
  onDone,
  onCancel,
}: {
  kind: EventKind;
  onDone: () => void;
  onCancel: () => void;
}) {
  const t = useT();
  const toast = useToast();
  const client = useQueryClient();
  const formRef = useRef<HTMLFormElement>(null);
  const act = useAction();
  const title = useKindTitle();
  const fieldErrors = useFieldErrors(act.error);
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const data = new FormData(e.currentTarget);
    void act.run(async () => {
      await api.PUT("/api/events/kinds/{kind}", {
        params: { path: { kind: kind.key } },
        body: { name: text(data.get("name")).trim() },
      });
      void client.invalidateQueries({ queryKey: ["events"] });
      toast.show(t("core.events.kinds.renamed"));
      onDone();
    });
  };
  return (
    <form ref={formRef} onSubmit={submit} className="flex flex-col gap-3" noValidate>
      <ErrorSummary error={act.error} formRef={formRef} />
      <Field
        label={t("core.events.kinds.name")}
        name="name"
        required
        maxLength={60}
        autoComplete="off"
        defaultValue={title(kind)}
        error={fieldErrors["name"]}
      />
      <div className="flex flex-wrap gap-3">
        <Button type="submit" busy={act.busy}>
          {t("core.events.kinds.save")}
        </Button>
        <Button variant="secondary" onClick={onCancel}>
          {t("core.events.cancel")}
        </Button>
      </div>
    </form>
  );
}
