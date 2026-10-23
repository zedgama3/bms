import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError, useReadOnly } from "@/api";
import { useT } from "@/i18n";
import { Button, Check, EmptyState, Field, Loading, useToast } from "@/ui";
import { controlClass } from "@/ui";
import { vol } from "./calls";
import { ConfirmDialog } from "./ConfirmDialog";
import { FormDialog } from "./FormDialog";
import { text } from "./helpers";
import { ReadOnlyNote } from "./ReadOnlyNote";
import { ScreenError } from "./ScreenError";
import type { Qualification } from "./types";

/** The qualification catalogue: a department's own entries (`department` set, with the
 *  org-wide ones listed read-only) or the org-wide ones (no `department`). `canManage` shows
 *  new, edit and archive; the server decides every call. */
export function CatalogueEditor({
  department,
  canManage,
}: {
  department?: string;
  canManage: boolean;
}) {
  const t = useT();
  const toast = useToast();
  const client = useQueryClient();
  const { readOnly } = useReadOnly();
  const [archived, setArchived] = useState(false);
  const [editing, setEditing] = useState<Qualification | "new" | null>(null);
  const [archiving, setArchiving] = useState<Qualification | null>(null);

  const q = useQuery({
    queryKey: ["volunteers", "qualifications", department ?? "org", archived],
    queryFn: ({ signal }) =>
      vol.qualifications(
        { ...(department ? { department } : {}), archived: archived && canManage },
        { signal },
      ),
  });
  const error = q.error instanceof ApiError ? q.error : null;
  const all = q.data ?? [];
  const own = all.filter((x) => (department ? x.department === department : !x.department));
  const orgWide = department ? all.filter((x) => !x.department) : [];

  const refresh = () => client.invalidateQueries({ queryKey: ["volunteers"] });
  const save = async (data: FormData) => {
    const days = text(data.get("validity_days")).trim();
    const body = {
      ...(department ? { department } : {}),
      name: text(data.get("name")).trim(),
      description: text(data.get("description")).trim(),
      ...(days ? { validity_days: Number(days) } : {}),
    };
    if (editing && editing !== "new") {
      await vol.updateQualification(editing.id, {
        ...body,
        ...(editing.department ? { department: editing.department } : {}),
      });
      toast.show(t("module.volunteers.catalogue.saved"));
    } else {
      await vol.createQualification(body);
      toast.show(t("module.volunteers.catalogue.created"));
    }
    await refresh();
  };

  const item = (x: Qualification, actions: boolean) => (
    <li key={x.id} className="flex flex-col gap-2 rounded-md border border-border bg-surface p-3">
      <h3 className="text-lg font-bold">
        {x.name}
        {x.archived_at ? (
          <span className="ms-2 text-sm font-normal text-muted">
            ({t("module.volunteers.catalogue.archived")})
          </span>
        ) : null}
      </h3>
      {x.description ? <p className="whitespace-pre-line">{x.description}</p> : null}
      <p className="text-muted">
        {x.validity_days
          ? t("module.volunteers.catalogue.validity", { days: x.validity_days })
          : t("module.volunteers.catalogue.indefinite")}
      </p>
      {actions && canManage && !x.archived_at ? (
        <div className="flex flex-wrap gap-3">
          <Button
            variant="secondary"
            disabled={readOnly}
            {...(readOnly ? { "aria-describedby": "catalogue-readonly" } : {})}
            onClick={() => setEditing(x)}
          >
            {t("module.volunteers.catalogue.edit")}
            <span className="sr-only"> {x.name}</span>
          </Button>
          <Button
            variant="danger"
            disabled={readOnly}
            {...(readOnly ? { "aria-describedby": "catalogue-readonly" } : {})}
            onClick={() => setArchiving(x)}
          >
            {t("module.volunteers.catalogue.archive")}
            <span className="sr-only"> {x.name}</span>
          </Button>
        </div>
      ) : null}
    </li>
  );

  const current = editing && editing !== "new" ? editing : null;
  return (
    <div className="flex flex-col gap-4">
      {canManage ? (
        <div className="flex flex-wrap items-center gap-3">
          <Button
            disabled={readOnly}
            {...(readOnly ? { "aria-describedby": "catalogue-readonly" } : {})}
            onClick={() => setEditing("new")}
          >
            {t("module.volunteers.catalogue.new")}
          </Button>
          <Check
            checked={archived}
            onChange={(e) => setArchived(e.target.checked)}
            label={t("module.volunteers.catalogue.show_archived")}
          />
        </div>
      ) : null}
      <ReadOnlyNote id="catalogue-readonly" readOnly={readOnly} />
      {q.isPending ? <Loading /> : null}
      {error ? <ScreenError error={error} onRetry={() => void q.refetch()} /> : null}
      {q.data && own.length === 0 ? (
        <EmptyState>
          {t(
            canManage
              ? "module.volunteers.catalogue.empty_manage"
              : "module.volunteers.catalogue.empty",
          )}
        </EmptyState>
      ) : null}
      {own.length > 0 ? (
        <ul className="flex flex-col gap-3">{own.map((x) => item(x, true))}</ul>
      ) : null}
      {orgWide.length > 0 ? (
        <section aria-labelledby="catalogue-org" className="flex flex-col gap-3">
          <h2 id="catalogue-org" className="text-xl font-bold">
            {t("module.volunteers.catalogue.org_wide")}
          </h2>
          <p className="text-sm text-muted">{t("module.volunteers.catalogue.org_wide_hint")}</p>
          <ul className="flex flex-col gap-3">{orgWide.map((x) => item(x, false))}</ul>
        </section>
      ) : null}

      <FormDialog
        open={editing !== null}
        title={t(
          current
            ? "module.volunteers.catalogue.edit_title"
            : "module.volunteers.catalogue.new_title",
        )}
        submit={t("module.volunteers.catalogue.save")}
        onClose={() => setEditing(null)}
        onSubmit={save}
      >
        {(errors) => (
          <>
            <Field
              label={t("module.volunteers.label.name")}
              name="name"
              required
              maxLength={100}
              autoComplete="off"
              defaultValue={current?.name ?? ""}
              error={errors["name"]}
            />
            <Field
              label={t("module.volunteers.label.description")}
              name="description"
              error={errors["description"]}
            >
              {(control) => (
                <textarea
                  {...control}
                  className={controlClass}
                  rows={3}
                  maxLength={2000}
                  autoComplete="off"
                  defaultValue={current?.description ?? ""}
                />
              )}
            </Field>
            <Field
              label={t("module.volunteers.label.validity_days")}
              name="validity_days"
              hint={t("module.volunteers.label.validity_days_hint")}
              type="number"
              inputMode="numeric"
              autoComplete="off"
              defaultValue={current?.validity_days ? String(current.validity_days) : ""}
              error={errors["validity_days"]}
            />
          </>
        )}
      </FormDialog>
      <ConfirmDialog
        open={archiving !== null}
        title={t("module.volunteers.catalogue.archive_title")}
        body={t("module.volunteers.catalogue.archive_body", { name: archiving?.name ?? "" })}
        confirm={t("module.volunteers.catalogue.archive_confirm")}
        returnFocus={department ? "#tab-heading" : "main h1"}
        onClose={() => setArchiving(null)}
        onConfirm={async () => {
          if (!archiving) return;
          await vol.archiveQualification(archiving.id);
          toast.show(t("module.volunteers.catalogue.archived_done"));
          await refresh();
        }}
      />
    </div>
  );
}
