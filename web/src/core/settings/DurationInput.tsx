import { useState } from "react";
import { useT } from "@/i18n";
import { controlClass } from "@/ui";
import type { ControlProps } from "@/ui";
import { composeDuration, parseDuration } from "./helpers";
import type { DurationUnit } from "./helpers";

/** A duration as a number and a unit (days, hours, minutes, seconds), sent as the API's text
 *  ("72h"; days are written as hours). Starts from `value` and keeps its own state after that:
 *  remount it (a new `key`) when the value is replaced from outside. */
export function DurationInput({
  control,
  value,
  onChange,
  label,
}: {
  control: ControlProps;
  value: string;
  onChange: (text: string) => void;
  /** The setting's title, to name the unit select. */
  label: string;
}) {
  const t = useT();
  const [state, setState] = useState(() => parseDuration(value));
  const update = (n: string, unit: DurationUnit) => {
    setState({ n, unit });
    onChange(composeDuration(n, unit));
  };
  return (
    <div className="flex gap-2">
      <input
        {...control}
        type="text"
        inputMode="numeric"
        pattern="[0-9]*"
        autoComplete="off"
        className={`${controlClass} flex-1`}
        value={state.n}
        onChange={(e) => update(e.target.value.replace(/\D/g, ""), state.unit)}
      />
      <select
        aria-label={t("core.settings.duration.unit", { setting: label })}
        className={`${controlClass} w-auto`}
        value={state.unit}
        onChange={(e) => update(state.n, e.target.value as DurationUnit)}
      >
        <option value="d">{t("core.settings.duration.days")}</option>
        <option value="h">{t("core.settings.duration.hours")}</option>
        <option value="m">{t("core.settings.duration.minutes")}</option>
        <option value="s">{t("core.settings.duration.seconds")}</option>
      </select>
    </div>
  );
}
