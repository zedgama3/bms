import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";
import { api, ApiError, useReadOnly, useSession } from "@/api";
import type { components } from "@/api";
import { useT } from "@/i18n";
import { Button, EmptyState, Loading, useToast } from "@/ui";
import { focusSoon } from "../focus";
import { ConfirmDialog } from "../ConfirmDialog";
import { Forbidden } from "../Forbidden";
import { editPath, holdsSite } from "../helpers";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { SendTest } from "../SendTest";
import { ScreenError } from "../ScreenError";
import { TEMPLATES_KEY, useTemplates } from "../queries";
import { useAction } from "../useAction";
import { useTemplateText } from "../useTemplateText";

type Info = components["schemas"]["NotifyTemplateInfo"];

/** `/notify/templates` (`notify.templates.manage`): every registered template grouped by owner,
 *  one link per language saying whether it is shipped or customised; then the customisations of
 *  templates nobody registers any more, each with a remove action. */
export function Templates() {
  const t = useT();
  const text = useTemplateText();
  const session = useSession();
  const toast = useToast();
  const client = useQueryClient();
  const { readOnly } = useReadOnly();
  const canTemplates = holdsSite(session, "notify.templates.manage");
  const canOutbox = holdsSite(session, "notify.outbox.manage");
  const q = useTemplates(canTemplates);
  const remove = useAction();
  const [removing, setRemoving] = useState<{ name: string; lang: string } | null>(null);

  if (!canTemplates) return <Forbidden title={t("core.notify.templates.title")} />;

  const error = q.error instanceof ApiError ? q.error : null;
  const all: readonly Info[] = q.data ?? [];
  const available = all.filter((i) => i.available);
  const gone = all.filter((i) => !i.available);
  const owners = [...new Set(available.map((i) => i.name.split(".", 1)[0] ?? ""))];

  const confirmRemove = () => {
    const target = removing;
    if (!target) return;
    void remove
      .run(async () => {
        await api.DELETE("/api/notify/templates/{name}/{lang}", {
          params: { path: target },
        });
        await client.invalidateQueries({ queryKey: TEMPLATES_KEY });
        toast.show(t("core.notify.templates.removed"));
      })
      .then((ok) => {
        if (ok) {
          setRemoving(null);
          focusSoon("#templates-title"); // the pressed Remove button is gone
        }
      });
  };

  return (
    <div className="flex flex-col gap-6" aria-busy={q.isPending || undefined}>
      <h1 id="templates-title" tabIndex={-1} className="text-2xl font-bold focus:outline-none">
        {t("core.notify.templates.title")}
      </h1>
      <p>{t("core.notify.templates.intro")}</p>
      <SendTest canOutbox={canOutbox} />
      {q.isPending ? <Loading /> : null}
      {error ? <ScreenError error={error} onRetry={() => void q.refetch()} /> : null}
      {q.data && all.length === 0 ? (
        <EmptyState>{t("core.notify.templates.empty")}</EmptyState>
      ) : null}
      {owners.map((owner) => (
        <section key={owner} aria-labelledby={`owner-${owner}`} className="flex flex-col gap-3">
          <h2 id={`owner-${owner}`} className="text-xl font-bold">
            {text.owner(owner)}
          </h2>
          <ul className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            {available
              .filter((i) => i.name.split(".", 1)[0] === owner)
              .map((i) => (
                <li key={i.name} className="rounded-md border border-border bg-surface p-4">
                  <h3 className="text-lg font-bold">{text.title(i.name)}</h3>
                  <p className="text-sm text-muted">
                    <code className="break-all">{i.name}</code>
                  </p>
                  <ul className="mt-2 flex flex-wrap gap-2">
                    {i.languages.map((l) => (
                      <li key={l.language}>
                        <Link
                          to={editPath(i.name, l.language)}
                          className="inline-flex min-h-11 items-center rounded-md border border-border px-3 underline"
                        >
                          {l.overridden
                            ? t("core.notify.templates.lang_customised", { lang: l.language })
                            : t("core.notify.templates.lang_shipped", { lang: l.language })}
                        </Link>
                      </li>
                    ))}
                  </ul>
                </li>
              ))}
          </ul>
        </section>
      ))}
      {gone.length > 0 ? (
        <section aria-labelledby="notify-gone" className="flex flex-col gap-3">
          <h2 id="notify-gone" className="text-xl font-bold">
            {t("core.notify.templates.gone_title")}
          </h2>
          <p>{t("core.notify.templates.gone_body")}</p>
          <ReadOnlyNote id="notify-gone-readonly" readOnly={readOnly} />
          <ul className="flex flex-col gap-3">
            {gone.flatMap((i) =>
              i.languages.map((l) => (
                <li
                  key={`${i.name}/${l.language}`}
                  className="flex flex-wrap items-center gap-3 rounded-md border border-border p-3"
                >
                  <span className="min-w-0 flex-1 break-all">
                    {t("core.notify.template_ref", { name: i.name, lang: l.language })}
                  </span>
                  <Button
                    variant="danger"
                    disabled={readOnly}
                    {...(readOnly ? { "aria-describedby": "notify-gone-readonly" } : {})}
                    aria-label={t("core.notify.templates.remove_label", {
                      name: i.name,
                      lang: l.language,
                    })}
                    onClick={() => {
                      remove.clear();
                      setRemoving({ name: i.name, lang: l.language });
                    }}
                  >
                    {t("core.notify.templates.remove")}
                  </Button>
                </li>
              )),
            )}
          </ul>
        </section>
      ) : null}
      <ConfirmDialog
        open={removing !== null}
        title={t("core.notify.templates.remove_title")}
        body={t("core.notify.templates.remove_body")}
        confirm={t("core.notify.templates.remove")}
        busy={remove.busy}
        error={remove.error}
        onConfirm={confirmRemove}
        onClose={() => setRemoving(null)}
      />
    </div>
  );
}
