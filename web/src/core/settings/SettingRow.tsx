import { useId, useState } from "react";
import type { ReactNode } from "react";
import { TriangleAlert } from "lucide-react";
import { useT } from "@/i18n";
import { Button, Check, controlClass, Field, FieldMessageText } from "@/ui";
import type { FieldMessage } from "@/ui";
import { DurationInput } from "./DurationInput";
import { byName, focusSoon } from "./focus";
import { hasStored, parseDuration, shownText } from "./helpers";
import { SecretControl } from "./SecretControl";
import type { AreaDoc, SettingDoc } from "./types";
import { useSettingsText } from "./useSettingsText";

/** What a control needs to draw one setting: its text, its value, how to change it. */
export interface ControlCtx {
  area: AreaDoc;
  setting: SettingDoc;
  /** Also the input's `name`: the API's field path for this setting. */
  name: string;
  label: string;
  hint: string;
  value: string;
  setValue: (text: string) => void;
  error: FieldMessage | undefined;
  optionLabel: (option: string) => string;
}

export type ControlRenderer = (c: ControlCtx) => ReactNode;

/** The control for a setting's kind (settings.md "UI"). Secrets are drawn by the row itself. */
function KindControl({ c }: { c: ControlCtx }) {
  const { setting: s } = c;
  const errId = useId();
  switch (s.kind) {
    case "bool":
      return (
        <div className="flex flex-col gap-1">
          <Check
            name={c.name}
            label={c.label}
            hint={c.hint}
            aria-describedby={c.error ? errId : undefined}
            aria-invalid={c.error ? true : undefined}
            checked={c.value === "true"}
            onChange={(e) => c.setValue(e.target.checked ? "true" : "false")}
          />
          {c.error ? (
            <p id={errId} className="text-sm font-semibold text-danger">
              <span aria-hidden="true">! </span>
              <FieldMessageText message={c.error} />
            </p>
          ) : null}
        </div>
      );
    case "duration":
      return (
        <Field label={c.label} name={c.name} hint={c.hint} error={c.error}>
          {(control) => (
            <DurationInput
              control={control}
              value={c.value}
              onChange={c.setValue}
              label={c.label}
            />
          )}
        </Field>
      );
    case "enum":
      return (
        <Field label={c.label} name={c.name} hint={c.hint} error={c.error}>
          {(control) => (
            <select
              {...control}
              className={controlClass}
              value={c.value}
              onChange={(e) => c.setValue(e.target.value)}
            >
              {(s.options ?? []).map((o) => (
                <option key={o} value={o}>
                  {c.optionLabel(o)}
                </option>
              ))}
            </select>
          )}
        </Field>
      );
    case "int":
      return (
        <Field
          label={c.label}
          name={c.name}
          hint={c.hint}
          error={c.error}
          inputMode="numeric"
          autoComplete="off"
          value={c.value}
          onChange={(e) => c.setValue(e.target.value)}
        />
      );
    default:
      return (
        <Field
          label={c.label}
          name={c.name}
          hint={c.hint}
          error={c.error}
          autoComplete="off"
          value={c.value}
          onChange={(e) => c.setValue(e.target.value)}
        />
      );
  }
}

/** One setting of a form: its control, the default, a stale warning and Reset to default. The
 *  draft is what the person typed (see `Drafts`). */
export function SettingRow({
  area,
  setting: s,
  draft,
  onDraft,
  error,
  control,
}: {
  area: AreaDoc;
  setting: SettingDoc;
  draft: string | null | undefined;
  onDraft: (v: string | null | undefined) => void;
  error: FieldMessage | undefined;
  control?: ControlRenderer | undefined;
}) {
  const t = useT();
  const text = useSettingsText();
  // A new key remounts the control that keeps its own state (a duration's unit) after a reset.
  const [epoch, setEpoch] = useState(0);
  const label = text.title(area, s);
  const hint = text.description(area, s);
  const value = shownText(s, draft);
  const ctx: ControlCtx = {
    area,
    setting: s,
    name: s.key,
    label,
    hint,
    value,
    setValue: onDraft,
    error,
    optionLabel: (o) => text.option(area, s, o),
  };

  const isSecret = s.kind === "secret";
  const defaultLine = (): string => {
    if (s.default === "") return t("core.settings.row.default_empty");
    switch (s.kind) {
      case "bool":
        return t("core.settings.row.default", {
          value:
            s.default === "true" ? t("core.settings.row.bool_on") : t("core.settings.row.bool_off"),
        });
      case "enum":
        return t("core.settings.row.default", { value: text.option(area, s, s.default) });
      case "duration": {
        const d = parseDuration(s.default);
        if (d.n === "0") {
          const word = text.zero(area, s);
          return word
            ? t("core.settings.row.default_named", { value: word })
            : t("core.settings.row.default_zero");
        }
        const unit = {
          d: t("core.settings.duration.days"),
          h: t("core.settings.duration.hours"),
          m: t("core.settings.duration.minutes"),
          s: t("core.settings.duration.seconds"),
        }[d.unit];
        return t("core.settings.row.default", { value: `${d.n} ${unit}` });
      }
      default:
        return t("core.settings.row.default", { value: s.default });
    }
  };

  const canReset = !isSecret && draft !== null && (hasStored(s) || value !== s.default);
  return (
    <div className="flex flex-col gap-2 border-t border-border pt-5 first:border-t-0 first:pt-0">
      {isSecret ? (
        <SecretControl
          name={s.key}
          label={label}
          hint={hint}
          isSet={s.secret_set}
          draft={draft}
          onDraft={onDraft}
          error={error}
        />
      ) : control ? (
        control(ctx)
      ) : (
        <KindControl key={epoch} c={ctx} />
      )}
      {!isSecret ? <p className="text-sm text-muted">{defaultLine()}</p> : null}
      {s.stale ? (
        <p className="flex items-start gap-2 text-sm font-semibold text-warn-fg">
          <TriangleAlert aria-hidden="true" className="mt-0.5 size-5 shrink-0" />
          <span>{t("core.settings.row.stale")}</span>
        </p>
      ) : null}
      {!isSecret && draft === null && hasStored(s) ? (
        <p className="text-sm font-semibold">{t("core.settings.row.reset_pending")}</p>
      ) : null}
      {canReset ? (
        <div>
          <Button
            variant="secondary"
            aria-label={t("core.settings.row.reset_label", { setting: label })}
            onClick={() => {
              onDraft(null);
              setEpoch((n) => n + 1);
              focusSoon(byName(s.key));
            }}
          >
            {t("core.settings.row.reset")}
          </Button>
        </div>
      ) : null}
    </div>
  );
}
