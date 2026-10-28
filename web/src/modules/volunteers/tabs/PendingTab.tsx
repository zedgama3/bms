import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";
import { ApiError, useNames, useReadOnly } from "@/api";
import { useT } from "@/i18n";
import { Button, EmptyState, Loading, useToast } from "@/ui";
import { vol } from "../calls";
import { ConfirmDialog } from "../ConfirmDialog";
import { nameIn, useQualifications } from "../queries";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { RecordCard } from "../RecordCard";
import { ScreenError } from "../ScreenError";
import type { QualRecord } from "../types";
import { VerifyDialog } from "../VerifyDialog";

/** Certificates waiting for a lead: open the file, correct the dates, verify or reject. */
export function PendingTab({ department }: { department: string }) {
  const t = useT();
  const toast = useToast();
  const client = useQueryClient();
  const { readOnly } = useReadOnly();
  const [verifying, setVerifying] = useState<QualRecord | null>(null);
  const [rejecting, setRejecting] = useState<QualRecord | null>(null);
  const q = useQuery({
    queryKey: ["volunteers", "pending", department],
    queryFn: ({ signal }) => vol.pending(department, { signal }),
  });
  const quals = useQualifications();
  const nameOfPerson = useNames(
    (q.data ?? []).map((r) => r.person),
    t("module.volunteers.person.unnamed"),
  );
  const error = q.error instanceof ApiError ? q.error : null;
  const qualName = (r: QualRecord) =>
    nameIn(quals.data, r.qualification) || t("module.volunteers.record.unknown_qualification");
  // Only once the catalogue has loaded: an unknown qualification is not called org-wide.
  const isOrgWide = (r: QualRecord) => {
    const found = quals.data?.find((x) => x.id === r.qualification);
    return found !== undefined && !found.department;
  };
  const ro = readOnly ? { "aria-describedby": "pending-readonly" } : {};

  return (
    <div className="flex flex-col gap-4">
      {q.isPending ? <Loading /> : null}
      {error ? <ScreenError error={error} onRetry={() => void q.refetch()} /> : null}
      {q.data && q.data.length === 0 ? (
        <EmptyState>{t("module.volunteers.pending.empty")}</EmptyState>
      ) : null}
      {q.data && q.data.length > 0 ? (
        <ul className="flex flex-col gap-3">
          {q.data.map((r) => (
            <RecordCard
              key={r.id}
              record={r}
              name={qualName(r)}
              orgWide={isOrgWide(r)}
              who={
                <Link
                  to={`/volunteers/people/${r.person}`}
                  className="inline-flex min-h-11 items-center underline"
                >
                  {nameOfPerson(r.person)}
                </Link>
              }
              actions={
                <>
                  <Button disabled={readOnly} {...ro} onClick={() => setVerifying(r)}>
                    {t("module.volunteers.pending.verify")}
                    <span className="sr-only"> {qualName(r)}</span>
                  </Button>
                  <Button
                    variant="danger"
                    disabled={readOnly}
                    {...ro}
                    onClick={() => setRejecting(r)}
                  >
                    {t("module.volunteers.pending.reject")}
                    <span className="sr-only"> {qualName(r)}</span>
                  </Button>
                </>
              }
            />
          ))}
        </ul>
      ) : null}
      <ReadOnlyNote id="pending-readonly" readOnly={readOnly} />
      <VerifyDialog
        record={verifying}
        name={verifying ? qualName(verifying) : ""}
        returnFocus="#tab-heading"
        onClose={() => setVerifying(null)}
      />
      <ConfirmDialog
        open={rejecting !== null}
        title={t("module.volunteers.pending.reject_title")}
        body={t("module.volunteers.pending.reject_body", {
          name: rejecting ? qualName(rejecting) : "",
        })}
        confirm={t("module.volunteers.pending.reject_confirm")}
        returnFocus="#tab-heading"
        onClose={() => setRejecting(null)}
        onConfirm={async () => {
          if (!rejecting) return;
          await vol.rejectRecord(rejecting.id);
          toast.show(t("module.volunteers.pending.rejected"));
          await client.invalidateQueries({ queryKey: ["volunteers"] });
        }}
      />
    </div>
  );
}
