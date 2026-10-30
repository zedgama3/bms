import { useId } from "react";
import { languageName, useFormat, useT } from "@/i18n";
import { controlClass, Field } from "@/ui";
import {
  contrastRatio,
  DARK_BACKGROUND,
  LIGHT_BACKGROUND,
  MIN_CONTRAST,
  parseHex,
} from "./helpers";
import type { ControlRenderer } from "./SettingRow";

/** The IANA zone names the browser knows, with the current value kept even when it is not one. */
function zones(current: string): string[] {
  let all: string[] = [];
  try {
    all = Intl.supportedValuesOf("timeZone");
  } catch {
    all = [];
  }
  return current !== "" && !all.includes(current) ? [current, ...all] : all;
}

/** A colour as a text field (#rrggbb), a native picker as its swatch, and the contrast against
 *  the shipped background, in words (the server checks it too, with the same 4.5 to 1). */
function ColorControl({
  c,
  background,
  dark,
}: {
  c: Parameters<ControlRenderer>[0];
  background: [number, number, number];
  dark: boolean;
}) {
  const t = useT();
  const format = useFormat();
  const id = useId();
  const statusId = `${id}-status`;
  const rgb = parseHex(c.value);
  const ratio = rgb ? contrastRatio(rgb, background) : null;
  // Rounded down, so a ratio that fails the check never shows as the minimum.
  const shown = ratio === null ? "" : format.number(Math.floor(ratio * 10) / 10);
  const bg = t(
    dark ? "core.settings.regional.background_dark" : "core.settings.regional.background_light",
  );
  const values = { ratio: shown, min: format.number(MIN_CONTRAST), background: bg };
  return (
    <Field
      label={c.label}
      name={c.name}
      hint={[c.hint, t("core.settings.regional.color_hint")].filter(Boolean).join(" ")}
      error={c.error}
    >
      {(control) => (
        <div className="flex flex-col gap-2">
          <div className="flex gap-2">
            <input
              {...control}
              aria-describedby={[control["aria-describedby"], statusId].filter(Boolean).join(" ")}
              type="text"
              inputMode="text"
              autoComplete="off"
              autoCapitalize="off"
              spellCheck={false}
              maxLength={7}
              placeholder="#rrggbb"
              className={`${controlClass} flex-1`}
              value={c.value}
              onChange={(e) => c.setValue(e.target.value)}
            />
            <input
              id={id}
              type="color"
              aria-label={t("core.settings.regional.color_picker", { setting: c.label })}
              className={`size-11 shrink-0 rounded-md border border-border bg-bg p-1 ${rgb ? "" : "opacity-40"}`}
              value={
                rgb && /^#[0-9a-f]{6}$/i.test(c.value.trim())
                  ? c.value.trim().toLowerCase()
                  : "#808080"
              }
              onChange={(e) => c.setValue(e.target.value)}
            />
          </div>
          {/* Always present, so the result is announced when it appears or changes. */}
          <div id={statusId} role="status">
            {ratio === null ? (
              <p className="text-sm text-muted">{t("core.settings.regional.color_none")}</p>
            ) : (
              <p className="text-sm font-semibold">
                {ratio >= MIN_CONTRAST
                  ? t("core.settings.regional.contrast_ok", values)
                  : t("core.settings.regional.contrast_low", values)}
              </p>
            )}
          </div>
        </div>
      )}
    </Field>
  );
}

/** The regional's own controls, by full key (settings.md "UI"): the rest use the kind's control. */
export const regionalControls: Readonly<Record<string, ControlRenderer>> = {
  "regional.contact_email": (c) => (
    <Field
      label={c.label}
      name={c.name}
      hint={c.hint}
      error={c.error}
      type="email"
      inputMode="email"
      autoComplete="off"
      value={c.value}
      onChange={(e) => c.setValue(e.target.value)}
    />
  ),
  "regional.public_url": (c) => (
    <Field
      label={c.label}
      name={c.name}
      hint={c.hint}
      error={c.error}
      type="url"
      inputMode="url"
      autoComplete="off"
      value={c.value}
      onChange={(e) => c.setValue(e.target.value)}
    />
  ),
  "regional.language": (c) => <LanguageSelect c={c} />,
  "regional.timezone": (c) => <TimezoneSelect c={c} />,
  "regional.color_primary": (c) => (
    <ColorControl c={c} background={LIGHT_BACKGROUND} dark={false} />
  ),
  "regional.color_primary_dark": (c) => <ColorControl c={c} background={DARK_BACKGROUND} dark />,
};

function LanguageSelect({ c }: { c: Parameters<ControlRenderer>[0] }) {
  return (
    <Field label={c.label} name={c.name} hint={c.hint} error={c.error}>
      {(control) => (
        <select
          {...control}
          className={controlClass}
          value={c.value}
          onChange={(e) => c.setValue(e.target.value)}
        >
          {(c.setting.options ?? []).map((tag) => (
            <option key={tag} value={tag} lang={tag}>
              {languageName(tag)}
            </option>
          ))}
        </select>
      )}
    </Field>
  );
}

function TimezoneSelect({ c }: { c: Parameters<ControlRenderer>[0] }) {
  const t = useT();
  return (
    <Field label={c.label} name={c.name} hint={c.hint} error={c.error}>
      {(control) => (
        <select
          {...control}
          className={controlClass}
          value={c.value}
          onChange={(e) => c.setValue(e.target.value)}
        >
          <option value="">{t("core.settings.regional.timezone_browser")}</option>
          {zones(c.value).map((z) => (
            <option key={z} value={z}>
              {z}
            </option>
          ))}
        </select>
      )}
    </Field>
  );
}
