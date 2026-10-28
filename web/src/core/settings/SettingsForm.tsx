import { useRef, useState } from "react";
import type { FormEvent } from "react";
import { api, useAction, useReadOnly } from "@/api";
import { useT } from "@/i18n";
import { Button, ErrorSummary, useFieldErrors, useToast } from "@/ui";
import { changesOf } from "./helpers";
import { ReadOnlyNote } from "./ReadOnlyNote";
import { DOCUMENT_KEY, useAfterSave } from "./queries";
import { SettingRow } from "./SettingRow";
import type { ControlRenderer } from "./SettingRow";
import type { AreaDoc, Drafts, SettingDoc } from "./types";

/** The form for some of one area's settings (`PUT /api/settings/values` with only the changed
 *  keys). Without `canManage` the controls are disabled and there is no Save: the server
 *  still decides. `controls` replaces the control of a setting, by full key. */
export function SettingsForm({
  area,
  settings,
  canManage,
  controls,
  legend,
}: {
  area: AreaDoc;
  settings: readonly SettingDoc[];
  canManage: boolean;
  controls?: Readonly<Record<string, ControlRenderer>>;
  legend: string;
}) {
  const t = useT();
  const toast = useToast();
  const afterSave = useAfterSave();
  const { readOnly } = useReadOnly();
  const formRef = useRef<HTMLFormElement>(null);
  const act = useAction();
  const fieldErrors = useFieldErrors(act.error);
  const [drafts, setDrafts] = useState<Drafts>({});

  const setDraft = (key: string, v: string | null | undefined) =>
    setDrafts((d) => {
      const next = { ...d };
      if (v === undefined) delete next[key];
      else next[key] = v;
      return next;
    });

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const values = changesOf(settings, drafts);
    if (Object.keys(values).length === 0) {
      toast.show(t("core.settings.nochanges"));
      return;
    }
    void act.run(async () => {
      await api.PUT("/api/settings/values", { body: { values } });
      await afterSave(DOCUMENT_KEY);
      setDrafts({});
      toast.show(t("core.settings.saved"));
    });
  };

  return (
    <form ref={formRef} onSubmit={submit} className="flex flex-col gap-5" noValidate>
      <ErrorSummary error={act.error} formRef={formRef} />
      <fieldset disabled={!canManage} className="m-0 flex min-w-0 flex-col gap-5 border-0 p-0">
        <legend className="sr-only">{legend}</legend>
        {settings.map((s) => (
          <SettingRow
            key={s.key}
            area={area}
            setting={s}
            draft={drafts[s.key]}
            onDraft={(v) => setDraft(s.key, v)}
            error={fieldErrors[s.key]}
            control={controls?.[s.key]}
          />
        ))}
      </fieldset>
      {canManage ? (
        <div className="sticky bottom-0 -mx-4 flex flex-col gap-1 border-t border-border bg-bg px-4 py-3">
          <ReadOnlyNote id={`settings-readonly-${area.name}`} readOnly={readOnly} />
          <div>
            <Button
              type="submit"
              busy={act.busy}
              disabled={readOnly}
              {...(readOnly ? { "aria-describedby": `settings-readonly-${area.name}` } : {})}
            >
              {t("core.settings.save")}
            </Button>
          </div>
        </div>
      ) : (
        <p className="text-sm text-muted">{t("core.settings.view_only")}</p>
      )}
    </form>
  );
}
