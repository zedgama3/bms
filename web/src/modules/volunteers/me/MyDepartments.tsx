import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError, useReadOnly } from "@/api";
import { useT } from "@/i18n";
import { Button, EmptyState, Loading, useToast } from "@/ui";
import { vol } from "../calls";
import { ConfirmDialog } from "../ConfirmDialog";
import { nameIn, useDepartments } from "../queries";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { ScreenError } from "../ScreenError";
import type { Member } from "../types";

/** The departments the person belongs to, each with Leave. */
export function MyDepartments() {
  const t = useT();
  const toast = useToast();
  const client = useQueryClient();
  const { readOnly } = useReadOnly();
  const [target, setTarget] = useState<Member | null>(null);
  const q = useQuery({
    queryKey: ["volunteers", "my-departments"],
    queryFn: ({ signal }) => vol.myDepartments({ signal }),
  });
  const departments = useDepartments();
  const error = q.error instanceof ApiError ? q.error : null;
  const nameOf = (m: Member) =>
    nameIn(departments.data, m.department) || t("module.volunteers.unknown_department");

  return (
    <section aria-labelledby="me-departments" className="flex flex-col gap-3">
      <h2 id="me-departments" className="text-xl font-bold">
        {t("module.volunteers.me.departments")}
      </h2>
      {q.isPending ? <Loading /> : null}
      {error ? <ScreenError error={error} onRetry={() => void q.refetch()} /> : null}
      {q.data && q.data.length === 0 ? (
        <EmptyState>{t("module.volunteers.me.no_departments")}</EmptyState>
      ) : null}
      {q.data && q.data.length > 0 ? (
        <ul className="flex flex-col gap-2">
          {q.data.map((m) => (
            <li
              key={m.id}
              className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-border bg-surface p-3"
            >
              <span className="min-w-0 font-semibold">{nameOf(m)}</span>
              <Button
                variant="secondary"
                disabled={readOnly}
                {...(readOnly ? { "aria-describedby": "departments-readonly" } : {})}
                onClick={() => setTarget(m)}
              >
                {t("module.volunteers.me.leave")}
                <span className="sr-only"> {nameOf(m)}</span>
              </Button>
            </li>
          ))}
        </ul>
      ) : null}
      <ReadOnlyNote id="departments-readonly" readOnly={readOnly} />
      <ConfirmDialog
        open={target !== null}
        title={t("module.volunteers.me.leave_title")}
        body={t("module.volunteers.me.leave_body", { department: target ? nameOf(target) : "" })}
        confirm={t("module.volunteers.me.leave_confirm")}
        returnFocus="#me-departments"
        onClose={() => setTarget(null)}
        onConfirm={async () => {
          if (!target) return;
          await vol.leave(target.department);
          toast.show(t("module.volunteers.me.left"));
          await client.invalidateQueries({ queryKey: ["volunteers"] });
        }}
      />
    </section>
  );
}
