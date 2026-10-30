import { Fragment, useRef, useState } from "react";
import type { FormEvent } from "react";
import { Link } from "react-router";
import { api, ApiError, useAction, useReadOnly } from "@/api";
import { isMessageKey, languageName, useLanguage, useT } from "@/i18n";
import {
  Banner,
  Button,
  controlClass,
  EmptyState,
  ErrorSummary,
  Field,
  Loading,
  useFieldErrors,
  useToast,
} from "@/ui";
import { Forbidden } from "../Forbidden";
import { byName, focusSoon } from "../focus";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { ScreenError } from "../ScreenError";
import { TERMS_KEY, useAfterSave, useTermsDocument } from "../queries";
import { FORMS } from "../types";
import type { Form, FormChanges, TermsDocument } from "../types";
import { useGate } from "../useGate";

type TermDocs = TermsDocument["terms"][string];

/** The API's field path for one form of one term. */
const fieldOf = (term: string, form: Form): string => `terms.${term}.${form}`;

interface TermGroup {
  area: string;
  title: string;
  names: string[];
}

/** The terms by area: "core" first, then each module's area alphabetically by its title, and
 *  the terms of an area by key (the document is a map, so its order is not defined). */
function groupTerms(terms: TermDocs, titleOf: (area: string) => string): TermGroup[] {
  const by = new Map<string, string[]>();
  for (const [name, doc] of Object.entries(terms)) {
    by.set(doc.area, [...(by.get(doc.area) ?? []), name]);
  }
  const out: TermGroup[] = [];
  for (const [area, names] of by) {
    out.push({ area, title: area === "core" ? "" : titleOf(area), names: names.sort() });
  }
  return out.sort((a, b) =>
    a.area === "core" || b.area === "core"
      ? Number(b.area === "core") - Number(a.area === "core")
      : a.title.localeCompare(b.title) || a.area.localeCompare(b.area),
  );
}

/** `/settings/terms` (`settings.read`): per language, the four forms of each term, the default
 *  under each box, Reset per box and one Save per language (`PUT /api/settings/terms/{lang}`).
 *  An empty box means "use the default". */
export function Terms() {
  const t = useT();
  const { canRead, canManage } = useGate();
  const q = useTermsDocument(canRead);
  const { current } = useLanguage();
  const [chosen, setChosen] = useState<string | null>(null);
  // Unsaved edits are kept per language, so switching language loses nothing.
  const [byLang, setByLang] = useState<Record<string, Record<string, string>>>({});

  if (!canRead) {
    return canManage ? (
      <div className="flex flex-col gap-4">
        <h1 className="text-2xl font-bold">{t("core.settings.terms.title")}</h1>
        <Banner tone="warn" live={false}>
          {t("core.settings.manage_only")}
        </Banner>
      </div>
    ) : (
      <Forbidden title={t("core.settings.terms.title")} />
    );
  }

  const error = q.error instanceof ApiError ? q.error : null;
  const doc = q.data;
  const languages = doc?.languages ?? [];
  const lang =
    chosen !== null && languages.includes(chosen)
      ? chosen
      : languages.includes(current)
        ? current
        : (languages[0] ?? "");
  const terms = doc?.terms[lang];

  return (
    <div className="flex flex-col gap-4" aria-busy={q.isPending || undefined}>
      <h1 className="text-2xl font-bold">{t("core.settings.terms.title")}</h1>
      <div>
        <Link to="/settings" className="inline-flex min-h-11 items-center underline">
          {t("core.settings.back")}
        </Link>
      </div>
      {q.isPending ? <Loading /> : null}
      {error ? <ScreenError error={error} onRetry={() => void q.refetch()} /> : null}
      {doc ? <p>{t("core.settings.terms.intro")}</p> : null}
      {doc && languages.length > 1 ? (
        <Field label={t("core.settings.terms.language")} name="language">
          {(control) => (
            <select
              {...control}
              className={controlClass}
              value={lang}
              onChange={(e) => setChosen(e.target.value)}
            >
              {languages.map((tag) => (
                <option key={tag} value={tag} lang={tag}>
                  {languageName(tag)}
                </option>
              ))}
            </select>
          )}
        </Field>
      ) : null}
      {doc && (!terms || Object.keys(terms).length === 0) ? (
        <EmptyState>{t("core.settings.terms.empty")}</EmptyState>
      ) : null}
      {terms ? (
        <TermsForm
          key={lang}
          lang={lang}
          terms={terms}
          canManage={canManage}
          drafts={byLang[lang] ?? {}}
          setDrafts={(next) => setByLang((all) => ({ ...all, [lang]: next(all[lang] ?? {}) }))}
        />
      ) : null}
    </div>
  );
}

function TermsForm({
  lang,
  terms,
  canManage,
  drafts,
  setDrafts,
}: {
  lang: string;
  terms: TermDocs;
  canManage: boolean;
  /** By field path; absent: untouched. "" (or only spaces): use the default. */
  drafts: Readonly<Record<string, string>>;
  setDrafts: (next: (d: Record<string, string>) => Record<string, string>) => void;
}) {
  const t = useT();
  const toast = useToast();
  const afterSave = useAfterSave();
  const { readOnly } = useReadOnly();
  const formRef = useRef<HTMLFormElement>(null);
  const act = useAction();
  const fieldErrors = useFieldErrors(act.error);
  const names = Object.keys(terms).sort();
  const groups = groupTerms(terms, (name) => {
    const key = `module.${name}.title`;
    return isMessageKey(key) ? t(key) : t("core.settings.area.fallback", { area: name });
  });

  const current = (term: string, form: Form): string =>
    drafts[fieldOf(term, form)] ?? terms[term]?.override[form] ?? "";

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const body: Record<string, FormChanges> = {};
    for (const term of names) {
      const change: { [F in Form]?: string } = {};
      const reset: Form[] = [];
      for (const form of FORMS) {
        const draft = drafts[fieldOf(term, form)];
        if (draft === undefined) continue;
        const was = terms[term]?.override[form] ?? null;
        const next = draft.trim();
        if (next === "") {
          if (was !== null) reset.push(form);
        } else if (next !== was) {
          change[form] = next;
        }
      }
      if (reset.length > 0 || Object.keys(change).length > 0) {
        body[term] = { ...change, ...(reset.length > 0 ? { reset } : {}) };
      }
    }
    if (Object.keys(body).length === 0) {
      toast.show(t("core.settings.nochanges"));
      return;
    }
    void act.run(async () => {
      await api.PUT("/api/settings/terms/{lang}", {
        params: { path: { lang } },
        body: { terms: body },
      });
      await afterSave(TERMS_KEY);
      setDrafts(() => ({}));
      toast.show(t("core.settings.saved"));
    });
  };

  const card = (term: string) => {
    const docs = terms[term]!;
    return (
      <fieldset
        key={term}
        className="flex min-w-0 flex-col gap-4 rounded-md border border-border bg-surface p-4"
      >
        <legend className="px-1 text-lg font-bold">{docs.default.singular_title}</legend>
        {FORMS.map((form) => {
          const name = fieldOf(term, form);
          const label = t(`core.settings.terms.form.${form}`);
          const value = current(term, form);
          return (
            <div key={form} className="flex flex-col gap-1">
              <Field
                label={label}
                name={name}
                hint={t("core.settings.terms.default", { value: docs.default[form] })}
                error={fieldErrors[name]}
                maxLength={60}
                autoComplete="off"
                value={value}
                onChange={(e) => setDrafts((d) => ({ ...d, [name]: e.target.value }))}
              />
              {value !== "" ? (
                <div>
                  <Button
                    variant="secondary"
                    aria-label={t("core.settings.terms.reset_label", {
                      field: `${docs.default.singular_title}, ${label}`,
                    })}
                    onClick={() => {
                      setDrafts((d) => ({ ...d, [name]: "" }));
                      focusSoon(byName(name));
                    }}
                  >
                    {t("core.settings.terms.reset")}
                  </Button>
                </div>
              ) : null}
            </div>
          );
        })}
      </fieldset>
    );
  };

  const langName = languageName(lang);
  const readOnlyId = "settings-terms-readonly";
  return (
    <form ref={formRef} onSubmit={submit} className="flex flex-col gap-5" noValidate>
      <ErrorSummary error={act.error} formRef={formRef} />
      <fieldset disabled={!canManage} className="m-0 flex min-w-0 flex-col gap-5 border-0 p-0">
        <legend className="sr-only">{t("core.settings.terms.title")}</legend>
        {groups.map((group) => {
          const cards = group.names.map((term) => card(term));
          return group.area === "core" ? (
            <Fragment key="core">{cards}</Fragment>
          ) : (
            <fieldset key={group.area} className="m-0 flex min-w-0 flex-col gap-5 border-0 p-0">
              <legend className="mb-3 text-xl font-bold">{group.title}</legend>
              {cards}
            </fieldset>
          );
        })}
      </fieldset>
      {canManage ? (
        <div className="sticky bottom-0 -mx-4 flex flex-col gap-1 border-t border-border bg-bg px-4 py-3">
          <ReadOnlyNote id={readOnlyId} readOnly={readOnly} />
          <div>
            <Button
              type="submit"
              busy={act.busy}
              disabled={readOnly}
              {...(readOnly ? { "aria-describedby": readOnlyId } : {})}
            >
              {t("core.settings.terms.save", { language: langName })}
            </Button>
          </div>
        </div>
      ) : (
        <p className="text-sm text-muted">{t("core.settings.view_only")}</p>
      )}
    </form>
  );
}
