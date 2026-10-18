import { useRef, useState } from "react";
import type { FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router";
import { api, ApiError, useReadOnly, useSession } from "@/api";
import { useFormat, useT } from "@/i18n";
import {
  Banner,
  Button,
  Dialog,
  ErrorSummary,
  Field,
  Loading,
  useFieldErrors,
  useToast,
} from "@/ui";
import { DetailsFields } from "../DetailsFields";
import { focusSoon } from "../focus";
import { holdsAt, holdsSite } from "../helpers";
import { Leadership } from "../Leadership";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { ScreenError } from "../ScreenError";
import { text } from "../text";
import { useAction } from "../useAction";

type Dialogs = "details" | "rename" | "archive" | "restore" | null;

/** `/departments/:department`: the details, the actions the session shows, the Leadership card.
 *  An archived department keeps its page for those allowed to see it, with a banner. */
export function DepartmentPage() {
  const t = useT();
  const format = useFormat();
  const toast = useToast();
  const client = useQueryClient();
  const session = useSession();
  const { readOnly } = useReadOnly();
  const { department = "" } = useParams();
  const [open, setOpen] = useState<Dialogs>(null);
  const detailsRef = useRef<HTMLFormElement>(null);
  const renameRef = useRef<HTMLFormElement>(null);
  const archiveRef = useRef<HTMLFormElement>(null);
  const restoreRef = useRef<HTMLFormElement>(null);
  const act = useAction();
  const fieldErrors = useFieldErrors(act.error);

  const q = useQuery({
    queryKey: ["departments", "one", department],
    queryFn: ({ signal }) =>
      api.GET("/api/departments/{department}", { signal, params: { path: { department } } }),
  });
  const d = q.data;
  const error = q.error instanceof ApiError ? q.error : null;
  const canManage = holdsSite(session, "departments.manage");
  const canDetails = holdsAt(session, "departments.details.manage", department);
  const canReadGrants = holdsAt(session, "access.grants.read", department);
  const canAppoint =
    holdsAt(session, "access.department-grants.manage", department) ||
    (session.signedIn && session.permissions.includes(`access.grants.manage@${department}`));

  const close = () => setOpen(null);
  const done = (message: string) => {
    toast.show(message);
    close();
    void client.invalidateQueries({ queryKey: ["departments"] });
    focusSoon("main h1");
  };
  const roDescribe = readOnly ? { "aria-describedby": "department-readonly" } : {};

  const saveDetails = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const data = new FormData(e.currentTarget);
    void act.run(async () => {
      await api.PUT("/api/departments/{department}", {
        params: { path: { department } },
        body: {
          description: text(data.get("description")).trim(),
          contact_email: text(data.get("contact_email")).trim(),
        },
      });
      done(t("core.departments.page.saved"));
    });
  };
  const rename = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const data = new FormData(e.currentTarget);
    void act.run(async () => {
      await api.PUT("/api/departments/{department}", {
        params: { path: { department } },
        body: { name: text(data.get("name")).trim() },
      });
      done(t("core.departments.page.renamed"));
    });
  };
  const toggle = (which: "archive" | "restore") => (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    void act.run(async () => {
      await api.POST(
        which === "archive"
          ? "/api/departments/{department}/archive"
          : "/api/departments/{department}/restore",
        { params: { path: { department } } },
      );
      done(
        which === "archive"
          ? t("core.departments.page.archived_done")
          : t("core.departments.page.restored_done"),
      );
    });
  };
  const openDialog = (which: Dialogs) => {
    act.clear();
    setOpen(which);
  };

  return (
    <div className="flex flex-col gap-4" aria-busy={q.isPending || undefined}>
      <div>
        <Link to="/departments" className="inline-flex min-h-11 items-center underline">
          {t("core.departments.page.back")}
        </Link>
      </div>
      {q.isPending ? <Loading /> : null}
      {error ? (
        <>
          <h1 className="text-2xl font-bold">{t("core.departments.page.title")}</h1>
          <ScreenError error={error} onRetry={() => void q.refetch()} />
        </>
      ) : null}
      {d ? (
        <>
          <h1 className="break-words text-2xl font-bold">{d.name}</h1>
          {d.archived ? (
            <Banner tone="warn">
              {t("core.departments.page.archived_on", {
                date: d.archived_at ? format.date(d.archived_at) : "",
              })}
            </Banner>
          ) : null}
          <section
            aria-labelledby="department-details"
            className="flex flex-col gap-2 rounded-md border border-border bg-surface p-4"
          >
            <h2 id="department-details" className="text-lg font-bold">
              {t("core.departments.page.details")}
            </h2>
            {d.description ? (
              <p className="whitespace-pre-line break-words">{d.description}</p>
            ) : (
              <p className="text-muted">{t("core.departments.page.no_description")}</p>
            )}
            {d.contact_email ? (
              <p>
                <a
                  href={`mailto:${d.contact_email}`}
                  className="inline-flex min-h-11 items-center break-all underline"
                >
                  {d.contact_email}
                </a>
              </p>
            ) : (
              <p className="text-muted">{t("core.departments.page.no_mailbox")}</p>
            )}
          </section>

          {(canDetails && !d.archived) || canManage ? (
            <section aria-labelledby="department-actions" className="flex flex-col gap-3">
              <h2 id="department-actions" className="sr-only">
                {t("core.departments.page.actions")}
              </h2>
              <div className="flex flex-wrap gap-3">
                {canDetails && !d.archived ? (
                  <Button
                    variant="secondary"
                    disabled={readOnly}
                    {...roDescribe}
                    onClick={() => openDialog("details")}
                  >
                    {t("core.departments.page.edit")}
                  </Button>
                ) : null}
                {canManage && !d.archived ? (
                  <Button
                    variant="secondary"
                    disabled={readOnly}
                    {...roDescribe}
                    onClick={() => openDialog("rename")}
                  >
                    {t("core.departments.page.rename")}
                  </Button>
                ) : null}
                {canManage && !d.archived ? (
                  <Button
                    variant="danger"
                    disabled={readOnly}
                    {...roDescribe}
                    onClick={() => openDialog("archive")}
                  >
                    {t("core.departments.page.archive")}
                  </Button>
                ) : null}
                {canManage && d.archived ? (
                  <Button disabled={readOnly} {...roDescribe} onClick={() => openDialog("restore")}>
                    {t("core.departments.page.restore")}
                  </Button>
                ) : null}
              </div>
              <ReadOnlyNote id="department-readonly" readOnly={readOnly} />
            </section>
          ) : null}

          <Leadership department={d} canRead={canReadGrants} canAppoint={canAppoint} />

          {canDetails && !d.archived ? (
            <Dialog
              open={open === "details"}
              title={t("core.departments.page.edit")}
              onClose={close}
            >
              <form
                key={d.updated_at}
                ref={detailsRef}
                onSubmit={saveDetails}
                className="flex flex-col gap-4"
                noValidate
              >
                {open === "details" ? (
                  <ErrorSummary error={act.error} formRef={detailsRef} />
                ) : null}
                <DetailsFields
                  description={d.description}
                  contactEmail={d.contact_email ?? ""}
                  errors={fieldErrors}
                />
                <div className="flex flex-wrap gap-3">
                  <Button type="submit" busy={act.busy}>
                    {t("core.departments.page.save")}
                  </Button>
                  <Button variant="secondary" onClick={close}>
                    {t("core.departments.cancel")}
                  </Button>
                </div>
              </form>
            </Dialog>
          ) : null}

          {canManage && !d.archived ? (
            <Dialog
              open={open === "rename"}
              title={t("core.departments.page.rename")}
              onClose={close}
            >
              <form
                key={d.updated_at}
                ref={renameRef}
                onSubmit={rename}
                className="flex flex-col gap-4"
                noValidate
              >
                {open === "rename" ? <ErrorSummary error={act.error} formRef={renameRef} /> : null}
                <Field
                  label={t("core.departments.field.name")}
                  name="name"
                  required
                  maxLength={100}
                  autoComplete="off"
                  defaultValue={d.name}
                  error={fieldErrors["name"]}
                />
                <div className="flex flex-wrap gap-3">
                  <Button type="submit" busy={act.busy}>
                    {t("core.departments.page.rename_confirm")}
                  </Button>
                  <Button variant="secondary" onClick={close}>
                    {t("core.departments.cancel")}
                  </Button>
                </div>
              </form>
            </Dialog>
          ) : null}

          {canManage && !d.archived ? (
            <Dialog
              open={open === "archive"}
              title={t("core.departments.page.archive_title")}
              onClose={close}
            >
              <form ref={archiveRef} onSubmit={toggle("archive")} className="flex flex-col gap-4">
                {open === "archive" ? (
                  <ErrorSummary error={act.error} formRef={archiveRef} />
                ) : null}
                <p>{t("core.departments.page.archive_body", { name: d.name })}</p>
                <div className="flex flex-wrap gap-3">
                  <Button type="submit" variant="danger" busy={act.busy}>
                    {t("core.departments.page.archive_confirm")}
                  </Button>
                  <Button variant="secondary" onClick={close}>
                    {t("core.departments.cancel")}
                  </Button>
                </div>
              </form>
            </Dialog>
          ) : null}

          {canManage && d.archived ? (
            <Dialog
              open={open === "restore"}
              title={t("core.departments.page.restore_title")}
              onClose={close}
            >
              <form ref={restoreRef} onSubmit={toggle("restore")} className="flex flex-col gap-4">
                {open === "restore" ? (
                  <ErrorSummary error={act.error} formRef={restoreRef} />
                ) : null}
                <p>{t("core.departments.page.restore_body", { name: d.name })}</p>
                <div className="flex flex-wrap gap-3">
                  <Button type="submit" busy={act.busy}>
                    {t("core.departments.page.restore_confirm")}
                  </Button>
                  <Button variant="secondary" onClick={close}>
                    {t("core.departments.cancel")}
                  </Button>
                </div>
              </form>
            </Dialog>
          ) : null}
        </>
      ) : null}
    </div>
  );
}
