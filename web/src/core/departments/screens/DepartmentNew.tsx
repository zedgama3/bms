import { useRef } from "react";
import type { FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router";
import { api, useReadOnly, useSession } from "@/api";
import { useT } from "@/i18n";
import { Button, ErrorSummary, Field, useFieldErrors, useToast } from "@/ui";
import { DetailsFields } from "../DetailsFields";
import { Forbidden } from "../Forbidden";
import { holdsSite } from "../helpers";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { text } from "../text";
import { useAction } from "../useAction";

/** `/departments/new` (`departments.manage`): name, description and the shared mailbox. */
export function DepartmentNew() {
  const t = useT();
  const toast = useToast();
  const navigate = useNavigate();
  const client = useQueryClient();
  const session = useSession();
  const { readOnly } = useReadOnly();
  const formRef = useRef<HTMLFormElement>(null);
  const act = useAction();
  const fieldErrors = useFieldErrors(act.error);

  if (!holdsSite(session, "departments.manage"))
    return <Forbidden title={t("core.departments.new.title")} />;

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const data = new FormData(e.currentTarget);
    const v = (name: string) => text(data.get(name)).trim();
    void act.run(async () => {
      const created = await api.POST("/api/departments/", {
        body: {
          name: v("name"),
          description: v("description"),
          ...(v("contact_email") ? { contact_email: v("contact_email") } : {}),
        },
      });
      void client.invalidateQueries({ queryKey: ["departments"] });
      toast.show(t("core.departments.new.done"));
      void navigate(`/departments/${created.id}`, { replace: true });
    });
  };

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-2xl font-bold">{t("core.departments.new.title")}</h1>
      <div>
        <Link to="/departments" className="inline-flex min-h-11 items-center underline">
          {t("core.departments.new.back")}
        </Link>
      </div>
      <form ref={formRef} onSubmit={submit} className="flex flex-col gap-5" noValidate>
        <ErrorSummary error={act.error} formRef={formRef} />
        <Field
          label={t("core.departments.field.name")}
          name="name"
          required
          maxLength={100}
          autoComplete="off"
          error={fieldErrors["name"]}
        />
        <DetailsFields errors={fieldErrors} />
        <ReadOnlyNote id="department-new-readonly" readOnly={readOnly} />
        <div>
          <Button
            type="submit"
            busy={act.busy}
            disabled={readOnly}
            {...(readOnly ? { "aria-describedby": "department-new-readonly" } : {})}
          >
            {t("core.departments.new.submit")}
          </Button>
        </div>
      </form>
    </div>
  );
}
