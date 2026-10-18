import { isMessageKey, useT } from "@/i18n";
import type { DeletionReport } from "./types";

/** What a deletion removed, kept or anonymised, in words. The report is shown once and never
 *  stored; it holds counts only. */
export function DeletionReportView({ report }: { report: DeletionReport }) {
  const t = useT();
  const kindOf = (kind: string) => {
    const key = `core.people.report.kind.${kind}`;
    return isMessageKey(key) ? t(key) : kind;
  };
  const moduleName = (name: string) => {
    const key = `module.${name}.title`;
    return isMessageKey(key) ? t(key) : name;
  };
  const modules = Object.entries(report.modules).sort(([a], [b]) => a.localeCompare(b));
  return (
    <div className="flex flex-col gap-3">
      <dl className="grid grid-cols-1 gap-2 sm:grid-cols-[auto_1fr] sm:gap-x-6">
        <Row label={t("core.people.report.people")} value={report.people} />
        <Row label={t("core.people.report.identity")} value={report.identity} />
        <Row label={t("core.people.report.access")} value={report.access} />
      </dl>
      {modules.length > 0 ? (
        <>
          <h3 className="font-semibold">{t("core.people.report.modules")}</h3>
          <ul className="flex flex-col gap-2">
            {modules.map(([name, m]) => (
              <li key={name} className="rounded-md border border-border p-3">
                <p className="font-semibold">{moduleName(name)}</p>
                <p>
                  {t("core.people.report.module_counts", {
                    deleted: m.deleted,
                    anonymized: m.anonymized,
                  })}
                </p>
                {m.kept.map((k) => (
                  <p key={k.kind} className="text-muted">
                    {t("core.people.report.kept", { kind: kindOf(k.kind), count: k.count })}
                  </p>
                ))}
              </li>
            ))}
          </ul>
        </>
      ) : null}
      {report.kept.length > 0 ? (
        <>
          <h3 className="font-semibold">{t("core.people.report.kept_title")}</h3>
          <ul className="list-disc ps-5">
            {report.kept.map((k) => (
              <li key={k.kind}>
                {t("core.people.report.kept", { kind: kindOf(k.kind), count: k.count })}
              </li>
            ))}
          </ul>
        </>
      ) : null}
    </div>
  );
}

function Row({ label, value }: { label: string; value: number }) {
  return (
    <div className="contents">
      <dt className="font-semibold">{label}</dt>
      <dd>{value}</dd>
    </div>
  );
}
