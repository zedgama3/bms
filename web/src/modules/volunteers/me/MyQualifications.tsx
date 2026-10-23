import { useRef, useState } from "react";
import type { FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError, useReadOnly, useSession } from "@/api";
import { useT } from "@/i18n";
import {
  Button,
  controlClass,
  DateField,
  EmptyState,
  ErrorSummary,
  Field,
  FileField,
  Loading,
  useFieldErrors,
  useToast,
} from "@/ui";
import { vol } from "../calls";
import { ConfirmDialog } from "../ConfirmDialog";
import { omitEmpty, text } from "../helpers";
import { nameIn, useQualifications } from "../queries";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { RecordCard } from "../RecordCard";
import { ScreenError } from "../ScreenError";
import type { QualRecord } from "../types";
import { useAction } from "../useAction";
import { useExpiry } from "../useExpiry";

const ACCEPT = ["image/jpeg", "image/png", "image/webp", "application/pdf"] as const;

/** My qualifications with their status, and **Upload a certificate** (a pending record a lead
 *  verifies). When the server answers `unsupported` (self-upload is off) the form gives way to
 *  a note. */
export function MyQualifications() {
  const t = useT();
  const toast = useToast();
  const client = useQueryClient();
  const session = useSession();
  const { readOnly } = useReadOnly();
  const me = session.signedIn ? session.personId : "";
  const formRef = useRef<HTMLFormElement>(null);
  const [file, setFile] = useState<File | null>(null);
  const [noFile, setNoFile] = useState(false);
  const [removing, setRemoving] = useState<QualRecord | null>(null);
  const act = useAction();
  const fieldErrors = useFieldErrors(act.error);

  const records = useQuery({
    queryKey: ["volunteers", "records", me],
    enabled: me !== "",
    queryFn: ({ signal }) => vol.recordsOf(me, { signal }),
  });
  const quals = useQualifications();
  const expiry = useExpiry(quals.data);
  const error = records.error instanceof ApiError ? records.error : null;
  const recordList = records.data?.items;
  // The upload action shows only when the person's own list says self-upload is on (V1.4); it
  // is never read from the settings route, which a volunteer may not call.
  const canUpload = records.data?.selfUpload === true;
  const nameOf = (r: QualRecord) =>
    nameIn(quals.data, r.qualification) || t("module.volunteers.record.unknown_qualification");

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = e.currentTarget;
    const data = new FormData(form);
    const v = (n: string) => text(data.get(n)).trim();
    if (!file) {
      setNoFile(true);
      const input = form.elements.namedItem("file");
      if (input instanceof HTMLElement) input.focus();
      return;
    }
    setNoFile(false);
    void act.run(async () => {
      await vol.uploadCertificate(
        file,
        omitEmpty({
          qualification: v("qualification"),
          acquired_on: v("acquired_on"),
          expires_on: v("expires_on"),
        }),
      );
      toast.show(t("module.volunteers.me.uploaded"));
      setFile(null);
      form.reset();
      expiry.reset();
      await client.invalidateQueries({ queryKey: ["volunteers"] });
    });
  };
  // Self-upload is a setting the server owns: its `unsupported` answer replaces the form.
  const off = act.error?.code === "unsupported";

  return (
    <section aria-labelledby="me-quals" className="flex flex-col gap-3">
      <h2 id="me-quals" className="text-xl font-bold">
        {t("module.volunteers.me.qualifications")}
      </h2>
      {records.isPending ? <Loading /> : null}
      {error ? <ScreenError error={error} onRetry={() => void records.refetch()} /> : null}
      {recordList && recordList.length === 0 ? (
        <EmptyState>{t("module.volunteers.me.no_qualifications")}</EmptyState>
      ) : null}
      {recordList && recordList.length > 0 ? (
        <ul className="flex flex-col gap-3">
          {recordList.map((r) => (
            <RecordCard
              key={r.id}
              record={r}
              name={nameOf(r)}
              actions={
                !r.verified_at && !r.verified_by ? (
                  <Button variant="secondary" disabled={readOnly} onClick={() => setRemoving(r)}>
                    {t("module.volunteers.record.withdraw")}
                    <span className="sr-only"> {nameOf(r)}</span>
                  </Button>
                ) : undefined
              }
            />
          ))}
        </ul>
      ) : null}

      {!records.data || !canUpload ? null : off ? (
        <p className="rounded-md border border-border bg-surface p-3">
          {t("module.volunteers.me.upload_off")}
        </p>
      ) : (
        <form
          ref={formRef}
          onSubmit={submit}
          noValidate
          aria-labelledby="me-upload"
          className="flex flex-col gap-4 rounded-md border border-border p-3"
        >
          <h3 id="me-upload" className="text-lg font-bold">
            {t("module.volunteers.me.upload")}
          </h3>
          <p className="text-sm text-muted">{t("module.volunteers.me.upload_hint")}</p>
          <ErrorSummary error={act.error} formRef={formRef} />
          <Field
            label={t("module.volunteers.label.qualification")}
            name="qualification"
            required
            error={fieldErrors["qualification"]}
          >
            {(control) => (
              <select
                {...control}
                className={controlClass}
                value={expiry.qualification.value}
                onChange={expiry.qualification.onChange}
              >
                <option value="" disabled>
                  {t("module.volunteers.choose")}
                </option>
                {(quals.data ?? []).map((q) => (
                  <option key={q.id} value={q.id}>
                    {q.name}
                  </option>
                ))}
              </select>
            )}
          </Field>
          <FileField
            label={t("module.volunteers.label.file")}
            name="file"
            accept={ACCEPT}
            value={file}
            onChange={(f) => {
              setFile(f);
              if (f) setNoFile(false);
            }}
            error={noFile ? { text: t("field.required") } : fieldErrors["file"]}
          />
          <DateField
            label={t("module.volunteers.label.acquired_on")}
            name="acquired_on"
            required
            value={expiry.acquired.value}
            onChange={expiry.acquired.onChange}
            error={fieldErrors["acquired_on"]}
          />
          <DateField
            label={t("module.volunteers.label.expires_on")}
            name="expires_on"
            hint={expiry.hint}
            value={expiry.expires.value}
            onChange={expiry.expires.onChange}
            error={fieldErrors["expires_on"]}
          />
          <ReadOnlyNote id="upload-readonly" readOnly={readOnly} />
          <div>
            <Button
              type="submit"
              busy={act.busy}
              disabled={readOnly}
              {...(readOnly ? { "aria-describedby": "upload-readonly" } : {})}
            >
              {t("module.volunteers.me.upload_submit")}
            </Button>
          </div>
        </form>
      )}
      <ConfirmDialog
        open={removing !== null}
        title={t("module.volunteers.record.withdraw_title")}
        body={t("module.volunteers.record.withdraw_body", {
          name: removing ? nameOf(removing) : "",
        })}
        confirm={t("module.volunteers.record.withdraw_confirm")}
        returnFocus="#me-quals"
        onClose={() => setRemoving(null)}
        onConfirm={async () => {
          if (!removing) return;
          await vol.removeRecord(removing.id);
          toast.show(t("module.volunteers.record.withdrawn"));
          await client.invalidateQueries({ queryKey: ["volunteers"] });
        }}
      />
    </section>
  );
}
