import { isMessageKey, useT } from "@/i18n";
import type { MessageKey } from "@/i18n";
import { bareKey } from "./helpers";
import type { AreaDoc, SettingDoc } from "./types";

export interface SettingsText {
  /** The setting's title; the raw key when the declaring area's catalogue lacks it. */
  title(area: AreaDoc, s: SettingDoc): string;
  /** The setting's description, or "" when there is none. */
  description(area: AreaDoc, s: SettingDoc): string;
  /** An enum option's label; the option itself when the catalogue lacks it. */
  option(area: AreaDoc, s: SettingDoc, option: string): string;
  /** What a zero duration means for this setting ("Kept forever"), or "" when the catalogue has no word. */
  zero(area: AreaDoc, s: SettingDoc): string;
  /** The area's name; the area's own name when no catalogue has a title for it. */
  areaTitle(area: AreaDoc): string;
}

function prefix(area: AreaDoc): string {
  return area.core ? `core.${area.name}` : `module.${area.name}`;
}

/** Each setting's text comes from its declaring area's catalogue (settings.md "UI"): core areas
 *  under `core.<area>.setting.<key>.*`, modules under `module.<area>.setting.<key>.*`. */
export function useSettingsText(): SettingsText {
  const t = useT();
  const look = (key: string): string | null => (isMessageKey(key) ? t(key) : null);
  return {
    title: (area, s) => look(`${prefix(area)}.setting.${bareKey(s.key)}.title`) ?? s.key,
    description: (area, s) => look(`${prefix(area)}.setting.${bareKey(s.key)}.description`) ?? "",
    option: (area, s, option) =>
      look(`${prefix(area)}.setting.${bareKey(s.key)}.option.${option}`) ?? option,
    zero: (area, s) => look(`${prefix(area)}.setting.${bareKey(s.key)}.zero`) ?? "",
    areaTitle: (area) => {
      const own = area.core
        ? look(`core.settings.area.${area.name}`)
        : look(`module.${area.name}.title`);
      return own ?? t("core.settings.area.fallback" satisfies MessageKey, { area: area.name });
    },
  };
}
