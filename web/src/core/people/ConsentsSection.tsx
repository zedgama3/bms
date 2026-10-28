import { useState } from "react";
import { useAction, useReadOnly } from "@/api";
import { isMessageKey, useFormat, useT } from "@/i18n";
import { Check, EmptyState, ErrorState, useToast } from "@/ui";
import { ReadOnlyNote } from "./ReadOnlyNote";
import { profileApi } from "./profileApi";
import type { Target } from "./profileApi";
import type { Consent } from "./types";

/** Each consent given, with a switch to withdraw (or give) it. A consent's title comes from the
 *  module that asks for it (`module.<owner>.consent.<ident>.title`); a purpose no catalogue
 *  knows shows as its name. */
export function ConsentsSection({
  consents,
  target,
  canEdit,
  onChanged,
}: {
  consents: readonly Consent[];
  target: Target;
  canEdit: boolean;
  onChanged: () => void;
}) {
  const t = useT();
  const format = useFormat();
  const toast = useToast();
  const { readOnly } = useReadOnly();
  const act = useAction();
  const [pending, setPending] = useState("");

  const titleOf = (purpose: string) => {
    const i = purpose.indexOf(".");
    const key = i < 0 ? "" : `module.${purpose.slice(0, i)}.consent.${purpose.slice(i + 1)}.title`;
    return isMessageKey(key) ? t(key) : purpose;
  };
  const change = (c: Consent, granted: boolean) => {
    setPending(c.purpose);
    void act
      .run(async () => {
        await profileApi.setConsent(target, c.purpose, granted);
        toast.show(granted ? t("core.people.consents.given") : t("core.people.consents.withdrawn"));
        onChanged();
      })
      .finally(() => setPending(""));
  };

  return (
    <section aria-labelledby="consents-h" className="flex flex-col gap-3">
      <h2 id="consents-h" className="text-xl font-bold">
        {t("core.people.consents.title")}
      </h2>
      {consents.length === 0 ? <EmptyState>{t("core.people.consents.empty")}</EmptyState> : null}
      {act.error ? <ErrorState error={act.error} /> : null}
      {consents.length > 0 ? (
        <ul className="flex flex-col gap-2">
          {consents.map((c) => (
            <li key={c.purpose} className="rounded-md border border-border bg-surface p-3">
              <Check
                checked={c.granted}
                disabled={readOnly || !canEdit || pending === c.purpose}
                {...(readOnly ? { "aria-describedby": "consents-readonly" } : {})}
                onChange={(e) => change(c, e.target.checked)}
                label={titleOf(c.purpose)}
                hint={t("core.people.consents.changed", { date: format.date(c.changed_at) })}
              />
            </li>
          ))}
        </ul>
      ) : null}
      <ReadOnlyNote id="consents-readonly" readOnly={readOnly} />
    </section>
  );
}
