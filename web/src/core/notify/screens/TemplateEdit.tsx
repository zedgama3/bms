import { useQueryClient } from "@tanstack/react-query";
import { useMemo, useRef, useState } from "react";
import type { FormEvent } from "react";
import { Link, useParams } from "react-router";
import { api, ApiError, useAction, useReadOnly, useSession } from "@/api";
import type { components } from "@/api";
import { useT } from "@/i18n";
import { Button, controlClass, ErrorSummary, Field, Loading, useFieldErrors, useToast } from "@/ui";
import { ConfirmDialog } from "../ConfirmDialog";
import { Forbidden } from "../Forbidden";
import { focusSoon } from "../focus";
import { holdsSite, STANDARD_FIELDS } from "../helpers";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { SendTest } from "../SendTest";
import { ScreenError } from "../ScreenError";
import { TEMPLATES_KEY, templateKey, useTemplate } from "../queries";
import { useTemplateText } from "../useTemplateText";

type View = components["schemas"]["NotifyTemplateView"];
type Rendered = components["schemas"]["NotifyRendered"];

/** `/notify/templates/:name/:lang` (`notify.templates.manage`). */
export function TemplateEdit() {
  const t = useT();
  const { name = "", lang = "" } = useParams();
  const session = useSession();
  const canTemplates = holdsSite(session, "notify.templates.manage");
  const q = useTemplate(name, lang, canTemplates);

  if (!canTemplates) return <Forbidden title={t("core.notify.edit.title")} />;
  const error = q.error instanceof ApiError ? q.error : null;
  return (
    <div className="flex flex-col gap-4" aria-busy={q.isPending || undefined}>
      {q.data ? null : <h1 className="text-2xl font-bold">{t("core.notify.edit.title")}</h1>}
      {q.isPending ? <Loading /> : null}
      {error ? (
        <>
          <BackLink />
          <ScreenError error={error} onRetry={() => void q.refetch()} />
        </>
      ) : null}
      {q.data ? <EditForm key={`${name}/${lang}`} view={q.data} /> : null}
    </div>
  );
}

function BackLink() {
  const t = useT();
  return (
    <div>
      <Link to="/notify/templates" className="inline-flex min-h-11 items-center underline">
        {t("core.notify.edit.back")}
      </Link>
    </div>
  );
}

/** A `parse` field error carries the parser's line in `params.line` when it reported one. The
 *  one message `module.notify.field.parse` selects on it, so a missing line becomes "none".
 *  Everything else about the error is left as it came. */
function withParseLine(error: ApiError | null): ApiError | null {
  if (!error || !error.fields.some((f) => f.code === "parse")) return error;
  const copy = new ApiError({
    status: error.status,
    code: error.code,
    ...(error.ref !== undefined ? { ref: error.ref } : {}),
    fields: error.fields.map((f) =>
      f.code === "parse" ? { ...f, params: { ...f.params, line: f.params?.line ?? "none" } } : f,
    ),
    method: error.method,
    route: error.route,
    module: "notify",
    traceId: error.traceId,
    retryAfter: error.retryAfter,
    detail: error.detail,
  });
  copy.reported = error.reported;
  return copy;
}

function EditForm({ view }: { view: View }) {
  const t = useT();
  const text = useTemplateText();
  const toast = useToast();
  const client = useQueryClient();
  const session = useSession();
  const { readOnly } = useReadOnly();
  const { name, language } = view;
  const formRef = useRef<HTMLFormElement>(null);
  const form = useAction(); // save and preview share the form's error summary
  const reset = useAction();
  const formError = useMemo(() => withParseLine(form.error), [form.error]);
  const fieldErrors = useFieldErrors(formError);
  const [subject, setSubject] = useState(view.current.subject);
  const [body, setBody] = useState(view.current.body);
  const [preview, setPreview] = useState<Rendered | null>(null);
  const [confirming, setConfirming] = useState(false);

  const store = (v: View) => {
    client.setQueryData(templateKey(name, language), v);
    void client.invalidateQueries({ queryKey: TEMPLATES_KEY });
  };
  const edited = (set: (v: string) => void) => (v: string) => {
    set(v);
    setPreview(null);
  };

  const save = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    void form.run(async () => {
      const saved = await api.PUT("/api/notify/templates/{name}/{lang}", {
        params: { path: { name, lang: language } },
        body: { subject, body },
      });
      if (saved) store(saved);
      toast.show(t("core.notify.edit.saved"));
    });
  };

  const showPreview = () => {
    void form.run(async () => {
      const out = await api.POST("/api/notify/templates/{name}/{lang}/preview", {
        params: { path: { name, lang: language } },
        body: { subject, body },
      });
      setPreview(out ?? null);
      focusSoon("#notify-preview");
    });
  };

  const doReset = () => {
    void reset
      .run(async () => {
        await api.DELETE("/api/notify/templates/{name}/{lang}", {
          params: { path: { name, lang: language } },
        });
        const fresh = await api.GET("/api/notify/templates/{name}/{lang}", {
          params: { path: { name, lang: language } },
        });
        if (fresh) {
          setSubject(fresh.current.subject);
          setBody(fresh.current.body);
          store(fresh);
        }
        setPreview(null);
        toast.show(t("core.notify.edit.was_reset"));
      })
      .then((ok) => {
        if (ok) {
          setConfirming(false);
          focusSoon('[name="subject"]');
        }
      });
  };

  const copy = (field: string) => {
    const snippet = `{{${field}}}`;
    void navigator.clipboard
      .writeText(snippet)
      .then(() => toast.show(t("core.notify.edit.copied", { field })))
      .catch(() => toast.show(t("core.notify.edit.copy_failed")));
  };

  const fields = [...STANDARD_FIELDS, ...view.vars.map((v) => `.Vars.${v}`)];

  return (
    <>
      <h1 className="text-2xl font-bold">{text.title(name)}</h1>
      <p className="text-sm text-muted">
        {t("core.notify.template_ref", { name, lang: language })}
      </p>
      <BackLink />
      <form ref={formRef} onSubmit={save} className="flex flex-col gap-4" noValidate>
        <ErrorSummary error={formError} formRef={formRef} />
        <Field
          label={t("core.notify.edit.subject")}
          name="subject"
          required
          error={fieldErrors["subject"]}
        >
          {(c) => (
            <input
              {...c}
              type="text"
              className={controlClass}
              value={subject}
              disabled={readOnly}
              onChange={(e) => edited(setSubject)(e.target.value)}
            />
          )}
        </Field>
        <Field label={t("core.notify.edit.body")} name="body" required error={fieldErrors["body"]}>
          {(c) => (
            <textarea
              {...c}
              rows={12}
              spellCheck={false}
              className={`${controlClass} font-mono`}
              value={body}
              disabled={readOnly}
              onChange={(e) => edited(setBody)(e.target.value)}
            />
          )}
        </Field>
        <ReadOnlyNote id="notify-edit-readonly" readOnly={readOnly} />
        <div className="flex flex-wrap gap-3">
          <Button
            type="submit"
            busy={form.busy}
            disabled={readOnly}
            {...(readOnly ? { "aria-describedby": "notify-edit-readonly" } : {})}
          >
            {t("core.notify.edit.save")}
          </Button>
          <Button variant="secondary" busy={form.busy} onClick={showPreview}>
            {t("core.notify.edit.preview")}
          </Button>
          {view.overridden ? (
            <Button
              variant="danger"
              disabled={readOnly}
              {...(readOnly ? { "aria-describedby": "notify-edit-readonly" } : {})}
              onClick={() => {
                reset.clear();
                setConfirming(true);
              }}
            >
              {t("core.notify.edit.reset")}
            </Button>
          ) : null}
        </div>
      </form>

      {preview ? (
        <section
          id="notify-preview"
          tabIndex={-1}
          aria-labelledby="notify-preview-title"
          className="flex flex-col gap-2 rounded-md border border-border p-4"
        >
          <h2 id="notify-preview-title" className="text-xl font-bold">
            {t("core.notify.edit.preview_title")}
          </h2>
          <p className="text-sm text-muted">{t("core.notify.edit.preview_note")}</p>
          <p className="font-semibold">
            {t("core.notify.edit.subject_line", { subject: preview.subject })}
          </p>
          <pre className="whitespace-pre-wrap break-words font-sans">{preview.body}</pre>
        </section>
      ) : null}

      <section aria-labelledby="notify-fields-title" className="flex flex-col gap-2">
        <h2 id="notify-fields-title" className="text-xl font-bold">
          {t("core.notify.edit.fields_title")}
        </h2>
        <p>{t("core.notify.edit.fields_body")}</p>
        <ul className="flex flex-wrap gap-2">
          {fields.map((f) => (
            <li key={f}>
              <Button
                variant="secondary"
                aria-label={t("core.notify.edit.copy_label", { field: f })}
                onClick={() => copy(f)}
              >
                <code>{f}</code>
              </Button>
            </li>
          ))}
        </ul>
      </section>

      <section aria-labelledby="notify-default-title" className="flex flex-col gap-2">
        <h2 id="notify-default-title" className="text-xl font-bold">
          {t("core.notify.edit.default_title")}
        </h2>
        <p className="text-sm text-muted">{t("core.notify.edit.default_note")}</p>
        <p className="font-semibold">
          {t("core.notify.edit.subject_line", { subject: view.default.subject })}
        </p>
        <pre className="whitespace-pre-wrap break-words rounded-md border border-border bg-surface p-3 font-mono text-sm">
          {view.default.body}
        </pre>
      </section>

      <SendTest canOutbox={holdsSite(session, "notify.outbox.manage")} />

      <ConfirmDialog
        open={confirming}
        title={t("core.notify.edit.reset_title")}
        body={t("core.notify.edit.reset_body")}
        confirm={t("core.notify.edit.reset")}
        busy={reset.busy}
        error={reset.error}
        onConfirm={doReset}
        onClose={() => setConfirming(false)}
      />
    </>
  );
}
