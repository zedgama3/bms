import { Link } from "react-router";
import { isMessageKey, useT } from "@/i18n";
import { EmptyState } from "@/ui";
import { safeHref } from "./helpers";
import type { Section } from "./types";

/** The modules' contributions to a person's page. A section's title is
 *  `module.<m>.section.<ident>.title`; a failed section says it couldn't be loaded and the rest
 *  of the page carries on. Item labels are message keys of the contributing module. */
export function SectionsView({ sections }: { sections: readonly Section[] }) {
  const t = useT();
  if (sections.length === 0) return null;
  return (
    <>
      {sections.map((s) => {
        const dot = s.key.indexOf(".");
        const mod = dot < 0 ? s.key : s.key.slice(0, dot);
        const ident = dot < 0 ? "" : s.key.slice(dot + 1);
        const titleKey = `module.${mod}.section.${ident}.title`;
        const title = isMessageKey(titleKey) ? t(titleKey) : s.key;
        const id = `section-${s.key.replace(/[^a-z0-9-]/gi, "-")}`;
        return (
          <section key={s.key} aria-labelledby={id} className="flex flex-col gap-3">
            <h2 id={id} className="text-xl font-bold">
              {title}
            </h2>
            {s.failed ? (
              <p role="status" className="font-semibold">
                {t("core.people.sections.failed")}
              </p>
            ) : s.items.length === 0 ? (
              <EmptyState>{t("core.people.sections.empty")}</EmptyState>
            ) : (
              <dl className="grid grid-cols-1 gap-3 rounded-md border border-border p-4 sm:grid-cols-[auto_1fr] sm:gap-x-6">
                {s.items.map((item, i) => {
                  const href = item.href ? safeHref(item.href) : "";
                  return (
                    <div key={`${item.label}-${i}`} className="contents">
                      <dt className="font-semibold">
                        {isMessageKey(item.label) ? t(item.label) : item.label}
                      </dt>
                      <dd className="break-words">
                        {href ? (
                          <Link to={href} className="inline-flex min-h-11 items-center underline">
                            {item.value}
                          </Link>
                        ) : (
                          item.value
                        )}
                      </dd>
                    </div>
                  );
                })}
              </dl>
            )}
          </section>
        );
      })}
    </>
  );
}
