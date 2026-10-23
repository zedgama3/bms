import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError, useReadOnly } from "@/api";
import { useT } from "@/i18n";
import { Button, Check, controlClass, EmptyState, Field, Loading, useToast } from "@/ui";
import { vol } from "../calls";
import { ConfirmDialog } from "../ConfirmDialog";
import { FormDialog } from "../FormDialog";
import { text } from "../helpers";
import { nameIn, useQualifications } from "../queries";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { ScreenError } from "../ScreenError";
import type { Position } from "../types";

type Mode =
  { kind: "edit"; position: Position | null } | { kind: "req"; position: Position } | null;

/** The department's positions: new, edit (with the **Hide who's on shifts from other
 *  volunteers** switch), the requirements picker and archive (`positions.manage`). */
export function PositionsTab({ department }: { department: string }) {
  const t = useT();
  const toast = useToast();
  const client = useQueryClient();
  const { readOnly } = useReadOnly();
  const [mode, setMode] = useState<Mode>(null);
  const [archiving, setArchiving] = useState<Position | null>(null);
  const q = useQuery({
    queryKey: ["volunteers", "positions", department],
    queryFn: ({ signal }) => vol.positions(department, false, { signal }),
  });
  const quals = useQualifications();
  const error = q.error instanceof ApiError ? q.error : null;
  const refresh = () => client.invalidateQueries({ queryKey: ["volunteers"] });
  const ro = readOnly ? { "aria-describedby": "positions-readonly" } : {};
  // A position may require an org-wide qualification or one of this department's.
  const qualsError = quals.error instanceof ApiError ? quals.error : null;
  const choices = (quals.data ?? []).filter((x) => !x.department || x.department === department);
  const editing = mode?.kind === "edit" ? mode.position : null;
  const reqFor = mode?.kind === "req" ? mode.position : null;

  return (
    <div className="flex flex-col gap-4">
      <div>
        <Button
          disabled={readOnly}
          {...ro}
          onClick={() => setMode({ kind: "edit", position: null })}
        >
          {t("module.volunteers.tab.positions.new")}
        </Button>
      </div>
      <ReadOnlyNote id="positions-readonly" readOnly={readOnly} />
      {q.isPending ? <Loading /> : null}
      {error ? <ScreenError error={error} onRetry={() => void q.refetch()} /> : null}
      {q.data && q.data.length === 0 ? (
        <EmptyState>{t("module.volunteers.tab.positions.empty")}</EmptyState>
      ) : null}
      {q.data && q.data.length > 0 ? (
        <ul className="flex flex-col gap-3">
          {q.data.map((p) => (
            <li
              key={p.id}
              className="flex flex-col gap-2 rounded-md border border-border bg-surface p-3"
            >
              <h3 className="text-lg font-bold">{p.name}</h3>
              {p.description ? <p className="whitespace-pre-line">{p.description}</p> : null}
              <p>{t(`module.volunteers.access.${p.access}`)}</p>
              {p.hide_shift_mates ? (
                <p className="font-semibold">{t("module.volunteers.tab.positions.hidden")}</p>
              ) : null}
              <p className="text-muted">
                {(p.requirements ?? []).length === 0
                  ? t("module.volunteers.tab.positions.no_requirements")
                  : t("module.volunteers.tab.positions.requires", {
                      names: (p.requirements ?? [])
                        .map(
                          (id) =>
                            nameIn(quals.data, id) || t("module.volunteers.why.a_qualification"),
                        )
                        .join(", "),
                    })}
              </p>
              <div className="flex flex-wrap gap-3">
                <Button
                  variant="secondary"
                  disabled={readOnly}
                  {...ro}
                  onClick={() => setMode({ kind: "edit", position: p })}
                >
                  {t("module.volunteers.tab.positions.edit")}
                  <span className="sr-only"> {p.name}</span>
                </Button>
                <Button
                  variant="secondary"
                  disabled={readOnly}
                  {...ro}
                  onClick={() => setMode({ kind: "req", position: p })}
                >
                  {t("module.volunteers.tab.positions.requirements")}
                  <span className="sr-only"> {p.name}</span>
                </Button>
                <Button
                  variant="danger"
                  disabled={readOnly}
                  {...ro}
                  onClick={() => setArchiving(p)}
                >
                  {t("module.volunteers.tab.positions.archive")}
                  <span className="sr-only"> {p.name}</span>
                </Button>
              </div>
            </li>
          ))}
        </ul>
      ) : null}

      <FormDialog
        open={mode?.kind === "edit"}
        title={t(
          editing
            ? "module.volunteers.tab.positions.edit_title"
            : "module.volunteers.tab.positions.new_title",
        )}
        submit={t("module.volunteers.tab.positions.save")}
        onClose={() => setMode(null)}
        onSubmit={async (data) => {
          const body = {
            name: text(data.get("name")).trim(),
            description: text(data.get("description")).trim(),
            access:
              text(data.get("access")) === "members" ? ("members" as const) : ("open" as const),
            hide_shift_mates: data.get("hide_shift_mates") !== null,
          };
          if (editing) await vol.updatePosition(editing.id, body);
          else await vol.createPosition(department, body);
          toast.show(t("module.volunteers.tab.positions.saved"));
          await refresh();
        }}
      >
        {(errors) => (
          <>
            <Field
              label={t("module.volunteers.label.name")}
              name="name"
              required
              maxLength={100}
              autoComplete="off"
              defaultValue={editing?.name ?? ""}
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
                  defaultValue={editing?.description ?? ""}
                />
              )}
            </Field>
            <Field
              label={t("module.volunteers.label.access")}
              name="access"
              error={errors["access"]}
            >
              {(control) => (
                <select
                  {...control}
                  className={controlClass}
                  defaultValue={editing?.access ?? "open"}
                >
                  <option value="open">{t("module.volunteers.access.open")}</option>
                  <option value="members">{t("module.volunteers.access.members")}</option>
                </select>
              )}
            </Field>
            <Check
              name="hide_shift_mates"
              defaultChecked={editing?.hide_shift_mates === true}
              label={t("module.volunteers.label.hide_shift_mates")}
              hint={t("module.volunteers.label.hide_shift_mates_hint")}
            />
          </>
        )}
      </FormDialog>

      <FormDialog
        open={mode?.kind === "req"}
        title={t("module.volunteers.req.title", { name: reqFor?.name ?? "" })}
        submit={t("module.volunteers.req.save")}
        submitDisabled={quals.data === undefined}
        onClose={() => setMode(null)}
        onSubmit={async (data) => {
          // Saving replaces the whole set, so it never runs on a catalogue that has not loaded.
          if (!reqFor || quals.data === undefined) return;
          const ids = data
            .getAll("qualifications")
            .filter((v): v is string => typeof v === "string");
          await vol.setRequirements(reqFor.id, ids);
          toast.show(t("module.volunteers.req.saved"));
          await refresh();
        }}
      >
        {() => (
          <fieldset className="flex flex-col gap-2">
            <legend className="mb-1 text-sm text-muted">{t("module.volunteers.req.hint")}</legend>
            {quals.isPending ? <Loading /> : null}
            {qualsError ? (
              <ScreenError error={qualsError} onRetry={() => void quals.refetch()} />
            ) : null}
            {quals.data && choices.length === 0 ? <p>{t("module.volunteers.req.none")}</p> : null}
            {choices.map((c) => (
              <Check
                key={c.id}
                name="qualifications"
                value={c.id}
                defaultChecked={(reqFor?.requirements ?? []).includes(c.id)}
                label={c.name}
                {...(c.department ? {} : { hint: t("module.volunteers.req.org_wide") })}
              />
            ))}
          </fieldset>
        )}
      </FormDialog>

      <ConfirmDialog
        open={archiving !== null}
        title={t("module.volunteers.tab.positions.archive_title")}
        body={t("module.volunteers.tab.positions.archive_body", { name: archiving?.name ?? "" })}
        confirm={t("module.volunteers.tab.positions.archive_confirm")}
        returnFocus="#tab-heading"
        onClose={() => setArchiving(null)}
        onConfirm={async () => {
          if (!archiving) return;
          await vol.archivePosition(archiving.id);
          toast.show(t("module.volunteers.tab.positions.archived_done"));
          await refresh();
        }}
      />
    </div>
  );
}
