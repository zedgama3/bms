import { useId } from "react";
import { api, isAbort, useReadOnly, useSetMe, useSession } from "@/api";
import { languageName, useLanguage, useT } from "@/i18n";
import { useToast } from "@/ui";

/** The language choice (decision 7). With one language on offer there is nothing to choose
 *  and nothing is rendered. A signed-in person's choice is also saved to their profile
 *  (`PUT /api/people/me {language}`); the browser key mirrors it either way. */
export function LanguageMenu() {
  const t = useT();
  const id = useId();
  const toast = useToast();
  const session = useSession();
  const setMe = useSetMe();
  const { readOnly } = useReadOnly();
  const { current, offered, set } = useLanguage();
  if (offered.length < 2) return null;

  const choose = (tag: string) => {
    set(tag);
    if (!session.signedIn || session.passwordChangeRequired || readOnly) return;
    api.PUT("/api/people/me", { body: { language: tag } }).then(setMe, (e: unknown) => {
      if (!isAbort(e)) toast.show(t("shell.language.unsaved"));
    });
  };
  return (
    <div className="flex items-center gap-2">
      <label htmlFor={id} className="sr-only">
        {t("shell.language")}
      </label>
      <select
        id={id}
        value={current}
        onChange={(e) => choose(e.target.value)}
        className="min-h-11 rounded-md border border-border bg-bg px-2 text-base"
      >
        {offered.map((tag) => (
          <option key={tag} value={tag} lang={tag}>
            {languageName(tag)}
          </option>
        ))}
      </select>
    </div>
  );
}
