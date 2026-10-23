import { useRef, useState } from "react";
import type { FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router";
import { ApiError, useReadOnly, useSession } from "@/api";
import { useT } from "@/i18n";
import {
  Button,
  controlClass,
  DateField,
  EmptyState,
  ErrorSummary,
  Field,
  Loading,
  useFieldErrors,
  useToast,
} from "@/ui";
import { vol } from "../calls";
import { ConfirmDialog } from "../ConfirmDialog";
import { Forbidden } from "../Forbidden";
import { holdsAny, omitEmpty, text } from "../helpers";
import { P } from "../permissions";
import { NeverExpires } from "../NeverExpires";
import { nameIn, useQualifications } from "../queries";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { RecordCard } from "../RecordCard";
import { ScreenError } from "../ScreenError";
import type { QualRecord } from "../types";
import { useAction } from "../useAction";
import { useExpiry } from "../useExpiry";
import { useNames } from "../useNames";
import { VerifyDialog } from "../VerifyDialog";

/** `/volunteers/people/:person`: a person's qualification records within the viewer's extent,
 *  **Record a qualification** (a lead records one from their own training records), open the
 *  certificate, verify or remove. A person outside the extent is a not-found state. */
export function PersonQualifications() {
  const t = useT();
  const toast = useToast();
  const client = useQueryClient();
  const session = useSession();
  const { readOnly } = useReadOnly();
  const { person = "" } = useParams();
  const formRef = useRef<HTMLFormElement>(null);
  const [removing, setRemoving] = useState<QualRecord | null>(null);
  const [verifying, setVerifying] = useState<QualRecord | null>(null);
  const act = useAction();
  const fieldErrors = useFieldErrors(act.error);

  const canRead =
    holdsAny(session, P.qualificationsRead) || holdsAny(session, P.qualificationsVerify);
  const canVerify = holdsAny(session, P.qualificationsVerify);
  const self = session.signedIn && session.personId === person;

  const records = useQuery({
    queryKey: ["volunteers", "records", person],
    enabled: canRead,
    queryFn: ({ signal }) => vol.recordsOf(person, { signal }),
  });
  const recordList = records.data?.items;
  const quals = useQualifications();
  const expiry = useExpiry(quals.data);
  const nameOfPerson = useNames([person], t("module.volunteers.person.unnamed"));
  const error = records.error instanceof ApiError ? records.error : null;
  const nameOf = (r: QualRecord) =>
    nameIn(quals.data, r.qualification) || t("module.volunteers.record.unknown_qualification");

  if (!canRead) return <Forbidden title={t("module.volunteers.person.title")} />;

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = e.currentTarget;
    const data = new FormData(form);
    const v = (n: string) => text(data.get(n)).trim();
    void act.run(async () => {
      await vol.recordQualification(
        person,
        omitEmpty({
          qualification: v("qualification"),
          acquired_on: v("acquired_on"),
          expires_on: v("expires_on"),
        }),
        data.get("no_expiry") !== null,
      );
      toast.show(t("module.volunteers.person.recorded"));
      form.reset();
      expiry.reset();
      await client.invalidateQueries({ queryKey: ["volunteers"] });
    });
  };

  return (
    <div className="flex flex-col gap-5" aria-busy={records.isPending || undefined}>
      <h1 className="text-2xl font-bold">
        {t("module.volunteers.person.heading", { name: nameOfPerson(person) })}
      </h1>
      <div>
        <Link to={`/people/${person}`} className="inline-flex min-h-11 items-center underline">
          {t("module.volunteers.person.profile")}
        </Link>
      </div>
      {records.isPending ? <Loading /> : null}
      {error ? <ScreenError error={error} onRetry={() => void records.refetch()} /> : null}
      {recordList && recordList.length === 0 ? (
        <EmptyState>{t("module.volunteers.person.empty")}</EmptyState>
      ) : null}
      {recordList && recordList.length > 0 ? (
        <ul className="flex flex-col gap-3">
          {recordList.map((r) => (
            <RecordCard
              key={r.id}
              record={r}
              name={nameOf(r)}
              actions={
                canVerify ? (
                  <>
                    {!r.verified_at && !r.verified_by ? (
                      <Button disabled={readOnly} onClick={() => setVerifying(r)}>
                        {t("module.volunteers.pending.verify")}
                        <span className="sr-only"> {nameOf(r)}</span>
                      </Button>
                    ) : null}
                    <Button variant="danger" disabled={readOnly} onClick={() => setRemoving(r)}>
                      {t("module.volunteers.person.remove")}
                      <span className="sr-only"> {nameOf(r)}</span>
                    </Button>
                  </>
                ) : undefined
              }
            />
          ))}
        </ul>
      ) : null}

      {canVerify && !self ? (
        <form
          ref={formRef}
          onSubmit={submit}
          noValidate
          aria-labelledby="person-record"
          className="flex flex-col gap-4 rounded-md border border-border p-3"
        >
          <h2 id="person-record" className="text-xl font-bold">
            {t("module.volunteers.person.record")}
          </h2>
          <p className="text-sm text-muted">{t("module.volunteers.person.record_hint")}</p>
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
            disabled={expiry.never.checked}
            error={fieldErrors["expires_on"]}
          />
          <NeverExpires checked={expiry.never.checked} onChange={expiry.never.onChange} />
          <ReadOnlyNote id="person-readonly" readOnly={readOnly} />
          <div>
            <Button
              type="submit"
              busy={act.busy}
              disabled={readOnly}
              {...(readOnly ? { "aria-describedby": "person-readonly" } : {})}
            >
              {t("module.volunteers.person.record_submit")}
            </Button>
          </div>
        </form>
      ) : null}

      <VerifyDialog
        record={verifying}
        name={verifying ? nameOf(verifying) : ""}
        returnFocus="main h1"
        onClose={() => setVerifying(null)}
      />
      <ConfirmDialog
        open={removing !== null}
        title={t("module.volunteers.person.remove_title")}
        body={t("module.volunteers.person.remove_body", { name: removing ? nameOf(removing) : "" })}
        confirm={t("module.volunteers.person.remove_confirm")}
        returnFocus="main h1"
        onClose={() => setRemoving(null)}
        onConfirm={async () => {
          if (!removing) return;
          await vol.removeRecord(removing.id);
          toast.show(t("module.volunteers.record.withdrawn"));
          await client.invalidateQueries({ queryKey: ["volunteers"] });
        }}
      />
    </div>
  );
}
