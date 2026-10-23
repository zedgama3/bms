import type { ReactNode } from "react";
import { useFormat, useT } from "@/i18n";
import { Attachments, todayDate } from "@/ui";
import { vol } from "./calls";
import { civilDate, recordStatus } from "./helpers";
import type { QualRecord } from "./types";

/** One qualification record: name, status (words, not colour), dates, the certificate link. */
export function RecordCard({
  record,
  name,
  actions,
  who,
  orgWide,
}: {
  record: QualRecord;
  name: string;
  actions?: ReactNode;
  /** Whose record it is, on lists that mix people. */
  who?: ReactNode;
  /** The qualification belongs to no department (D41). */
  orgWide?: boolean;
}) {
  const t = useT();
  const format = useFormat();
  const status = recordStatus(record, todayDate());
  return (
    <li className="flex flex-col gap-1 rounded-md border border-border bg-surface p-3">
      <h3 className="text-lg font-bold">{name}</h3>
      {orgWide ? (
        <p className="text-sm font-semibold text-muted">{t("module.volunteers.record.org_wide")}</p>
      ) : null}
      {who ? <p className="font-semibold">{who}</p> : null}
      <p className="font-semibold">{t(`module.volunteers.record.status.${status}`)}</p>
      <p>
        {t("module.volunteers.record.acquired", {
          date: format.date(civilDate(record.acquired_on)),
        })}
      </p>
      <p>
        {record.expires_on
          ? t("module.volunteers.record.expires", {
              date: format.date(civilDate(record.expires_on)),
            })
          : t("module.volunteers.record.no_expiry")}
      </p>
      {record.certificate ? (
        <Attachments
          goneMessage={t("module.volunteers.record.certificate_gone")}
          rows={[
            {
              id: record.id,
              label: t("module.volunteers.record.certificate", { name }),
              fetch: (signal) => vol.certificate(record.id, signal),
            },
          ]}
        />
      ) : null}
      {actions ? <div className="flex flex-wrap gap-3">{actions}</div> : null}
    </li>
  );
}
