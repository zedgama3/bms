import { useRef, useState } from "react";
import type { FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, Navigate, useParams } from "react-router";
import { api, ApiError, useAction, useReadOnly, useSession } from "@/api";
import { useT } from "@/i18n";
import {
  Banner,
  Button,
  Dialog,
  ErrorState,
  ErrorSummary,
  Field,
  linkButtonClass,
  Loading,
  useFieldErrors,
  useToast,
} from "@/ui";
import { focusSoon } from "../focus";
import { ConsentsSection } from "../ConsentsSection";
import { ContactsSection } from "../ContactsSection";
import { DeletionReportView } from "../DeletionReportView";
import { saveJson } from "../download";
import { ProfileForm } from "../ProfileForm";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { ScreenError } from "../ScreenError";
import { SectionsView } from "../SectionsView";
import { text } from "../text";
import type { DeletionReport, Profile } from "../types";

/** `/people/:person`: another person as the viewer may see them. What each part shows and which
 *  buttons exist come from the server's `can` and `fields`; the server decides every call. */
export function PersonPage() {
  const { person: id = "" } = useParams();
  const session = useSession();
  if (session.signedIn && session.personId === id) return <Navigate to="/people/me" replace />;
  return <Person id={id} />;
}

function Person({ id }: { id: string }) {
  const t = useT();
  const client = useQueryClient();
  const [report, setReport] = useState<DeletionReport | null>(null);
  const q = useQuery({
    queryKey: ["people", "person", id],
    enabled: report === null,
    queryFn: ({ signal }) =>
      api.GET("/api/people/{person}", { signal, params: { path: { person: id } } }),
  });
  const error = q.error instanceof ApiError ? q.error : null;
  const profile = q.data;
  // On another person's page `can.edit` already implies the manager permission; on one's own
  // ID it is also true for a non-manager, so ask the session.
  const session = useSession();
  const mayRename =
    !session.signedIn ||
    session.personId !== id ||
    session.permissions.includes("people.profiles.manage");

  if (report) {
    return (
      <div className="flex flex-col gap-4">
        <h1 className="text-2xl font-bold">{t("core.people.person.deleted_page")}</h1>
        <DeletionReportView report={report} />
        <div>
          <Link to="/people" className={linkButtonClass("primary")}>
            {t("core.people.report.link")}
          </Link>
        </div>
      </div>
    );
  }

  if (!profile) {
    return (
      <div className="flex flex-col gap-4" aria-busy={!error || undefined}>
        <h1 className="text-2xl font-bold">{t("core.people.person.title")}</h1>
        {error ? <ScreenError error={error} onRetry={() => void q.refetch()} /> : <Loading />}
        <BackLink />
      </div>
    );
  }

  const target = { person: id };
  const changed = () => void client.invalidateQueries({ queryKey: ["people", "person", id] });
  const has = (group: string) => profile.fields.includes(group);
  const { can, person } = profile;
  const personal = can.read_contacts;

  return (
    <div className="flex flex-col gap-8">
      <h1 className="break-words text-2xl font-bold">
        {person.playa_name || t("core.people.person.unnamed")}
      </h1>
      {error ? <ScreenError error={error} onRetry={() => void q.refetch()} /> : null}
      <BackLink />
      {personal ? <Banner live={false}>{t("core.people.person.recorded")}</Banner> : null}

      <section aria-labelledby="about-h" className="flex flex-col gap-3">
        <h2 id="about-h" className="text-xl font-bold">
          {t("core.people.profile.about")}
        </h2>
        {can.edit ? (
          <ProfileForm
            key={person.updated_at ?? "new"}
            profile={profile}
            target={target}
            canRename={mayRename}
            onSaved={(saved) => client.setQueryData(["people", "person", id], saved)}
          />
        ) : (
          <Details profile={profile} />
        )}
      </section>

      {has("emergency_contacts") && profile.policy.emergency_contacts !== "hidden" ? (
        <ContactsSection
          contacts={profile.contacts}
          target={target}
          canEdit={can.edit}
          onChanged={changed}
        />
      ) : null}
      {has("consents") ? (
        <ConsentsSection
          consents={profile.consents}
          target={target}
          canEdit={can.edit}
          onChanged={changed}
        />
      ) : null}
      <SectionsView sections={profile.sections} />
      <Actions
        id={id}
        profile={profile}
        onChanged={changed}
        onDeleted={(res) => {
          setReport(res);
          client.removeQueries({ queryKey: ["people", "person", id] });
          void client.invalidateQueries({ queryKey: ["people", "search"] });
          focusSoon("main h1");
        }}
      />
    </div>
  );
}

function BackLink() {
  const t = useT();
  return (
    <div>
      <Link to="/people" className="inline-flex min-h-11 items-center underline">
        {t("core.people.person.back")}
      </Link>
    </div>
  );
}

function Details({ profile }: { profile: Profile }) {
  const t = useT();
  const { person } = profile;
  const rows: [string, string][] = [
    [t("core.people.profile.playa_name"), person.playa_name],
    [t("core.people.profile.pronouns"), person.pronouns],
    [t("core.people.profile.legal_name"), person.legal_name ?? ""],
    [t("core.people.profile.phone"), person.phone ?? ""],
    [t("core.people.profile.contact_email"), person.contact_email ?? ""],
    [t("core.people.profile.login_email"), person.email ?? ""],
  ];
  return (
    <dl className="grid grid-cols-1 gap-3 rounded-md border border-border p-4 sm:grid-cols-[auto_1fr] sm:gap-x-6">
      {rows
        .filter(([, v]) => v !== "")
        .map(([label, value]) => (
          <div key={label} className="contents">
            <dt className="font-semibold">{label}</dt>
            <dd className="break-all">{value}</dd>
          </div>
        ))}
    </dl>
  );
}

type Dialogs = "link" | "delete" | null;

function Actions({
  id,
  profile,
  onChanged,
  onDeleted,
}: {
  id: string;
  profile: Profile;
  onChanged: () => void;
  onDeleted: (report: DeletionReport) => void;
}) {
  const t = useT();
  const toast = useToast();
  const { readOnly } = useReadOnly();
  const { can, person } = profile;
  const [open, setOpen] = useState<Dialogs>(null);
  const [linked, setLinked] = useState<boolean | null>(null);
  const linkRef = useRef<HTMLFormElement>(null);
  const deleteRef = useRef<HTMLFormElement>(null);
  const link = useAction();
  const del = useAction();
  const exporting = useAction();
  const fieldErrors = useFieldErrors(link.error);
  const describedBy = readOnly ? { "aria-describedby": "person-readonly" } : {};

  const anything = can.view_roles || can.view_audit || can.link || can.export || can.delete;
  if (!anything) return null;

  const submitLink = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const email = text(new FormData(e.currentTarget).get("email")).trim();
    void link.run(async () => {
      const res = await api.POST("/api/people/{person}/link-account", {
        params: { path: { person: id } },
        body: { email },
      });
      setLinked(res.orphan_deleted);
      setOpen(null);
      onChanged();
      focusSoon("#link-result");
    });
  };

  const submitDelete = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    void del.run(async () => {
      const res = await api.DELETE("/api/datarights/people/{person}", {
        params: { path: { person: id } },
      });
      setOpen(null);
      onDeleted(res);
    });
  };

  const download = () => {
    void exporting.run(async () => {
      const data = await api.GET("/api/datarights/people/{person}/export", {
        params: { path: { person: id } },
      });
      saveJson(data, `bms-export-${id}.json`);
      toast.show(t("core.people.person.downloaded"));
    });
  };

  return (
    <section aria-labelledby="actions-h" className="flex flex-col gap-3">
      <h2 id="actions-h" className="text-xl font-bold">
        {t("core.people.person.actions")}
      </h2>
      {linked !== null ? (
        <div id="link-result">
          <Banner>
            {linked ? t("core.people.person.linked_removed") : t("core.people.person.linked_kept")}
          </Banner>
        </div>
      ) : null}
      {exporting.error ? <ErrorState error={exporting.error} /> : null}
      <ul className="flex flex-col gap-3 sm:flex-row sm:flex-wrap">
        {can.view_roles ? (
          <li>
            <Link to={`/access/grants?person=${id}`} className={linkButtonClass("secondary")}>
              {t("core.people.person.roles")}
            </Link>
          </li>
        ) : null}
        {can.view_audit ? (
          <li>
            <Link to={`/audit?subject=${id}`} className={linkButtonClass("secondary")}>
              {t("core.people.person.audit")}
            </Link>
          </li>
        ) : null}
        {can.link && !person.has_account ? (
          <li>
            <Button
              variant="secondary"
              disabled={readOnly}
              {...describedBy}
              onClick={() => {
                link.clear();
                setOpen("link");
              }}
            >
              {t("core.people.person.link")}
            </Button>
          </li>
        ) : null}
        {can.export ? (
          <li>
            <Button variant="secondary" busy={exporting.busy} onClick={download}>
              {t("core.people.person.export")}
            </Button>
          </li>
        ) : null}
        {can.delete ? (
          <li>
            <Button
              variant="danger"
              disabled={readOnly}
              {...describedBy}
              onClick={() => {
                del.clear();
                setOpen("delete");
              }}
            >
              {t("core.people.person.delete")}
            </Button>
          </li>
        ) : null}
      </ul>
      <ReadOnlyNote id="person-readonly" readOnly={readOnly} />

      <Dialog
        open={open === "link"}
        title={t("core.people.person.link_title")}
        onClose={() => setOpen(null)}
      >
        <form ref={linkRef} onSubmit={submitLink} className="flex flex-col gap-4" noValidate>
          <ErrorSummary error={link.error} formRef={linkRef} />
          <p>{t("core.people.person.link_body")}</p>
          <Field
            label={t("core.people.person.link_email")}
            name="email"
            type="email"
            inputMode="email"
            autoComplete="off"
            required
            error={fieldErrors["email"]}
          />
          <div className="flex flex-wrap gap-3">
            <Button type="submit" busy={link.busy}>
              {t("core.people.person.link_submit")}
            </Button>
            <Button variant="secondary" onClick={() => setOpen(null)}>
              {t("core.people.cancel")}
            </Button>
          </div>
        </form>
      </Dialog>

      <Dialog
        open={open === "delete"}
        title={t("core.people.person.delete_title")}
        onClose={() => setOpen(null)}
      >
        <form ref={deleteRef} onSubmit={submitDelete} className="flex flex-col gap-4">
          <ErrorSummary error={del.error} formRef={deleteRef} />
          <p className="break-words font-semibold">
            {t("core.people.person.delete_name", {
              name: person.playa_name || t("core.people.person.unnamed"),
            })}
          </p>
          <p>{t("core.people.person.delete_list")}</p>
          <ul className="list-disc ps-5">
            <li>{t("core.people.person.delete_a")}</li>
            <li>{t("core.people.person.delete_b")}</li>
            <li>{t("core.people.person.delete_c")}</li>
            <li>{t("core.people.person.delete_d")}</li>
          </ul>
          <p className="font-semibold">{t("core.people.person.delete_final")}</p>
          <div className="flex flex-wrap gap-3">
            <Button type="submit" variant="danger" busy={del.busy}>
              {t("core.people.person.delete_confirm")}
            </Button>
            <Button variant="secondary" onClick={() => setOpen(null)}>
              {t("core.people.cancel")}
            </Button>
          </div>
        </form>
      </Dialog>
    </section>
  );
}
