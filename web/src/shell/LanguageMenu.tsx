import { useId } from "react";
import { useLanguage, useT } from "@/i18n";
import type { MessageKey } from "@/i18n";

/** The language choice (decision 7). With one language on offer there is nothing to choose
 *  and nothing is rendered. */
export function LanguageMenu() {
  const t = useT();
  const id = useId();
  const { current, offered, set } = useLanguage();
  if (offered.length < 2) return null;
  return (
    <div className="flex items-center gap-2">
      <label htmlFor={id} className="sr-only">
        {t("shell.language")}
      </label>
      <select
        id={id}
        value={current}
        onChange={(e) => set(e.target.value)}
        className="min-h-11 rounded-md border border-border bg-bg px-2 text-base"
      >
        {offered.map((tag) => (
          <option key={tag} value={tag} lang={tag}>
            {nameOf(t, tag)}
          </option>
        ))}
      </select>
    </div>
  );
}

function nameOf(t: ReturnType<typeof useT>, tag: string): string {
  const key = `shell.language.${tag}`;
  const name = t(key as MessageKey);
  return name === key ? tag : name;
}
