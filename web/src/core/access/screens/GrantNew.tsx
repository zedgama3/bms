import { useRef, useState } from "react";
import type { FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useSearchParams } from "react-router";
import { api, ApiError, searchPeople, useAction, useNames, useReadOnly } from "@/api";
import { useT } from "@/i18n";
import {
  Button,
  controlClass,
  DateField,
  endOfDayInstant,
  ErrorSummary,
  Field,
  Loading,
  todayDate,
  useFieldErrors,
  useToast,
  PersonSearch,
} from "@/ui";
import { useTitles } from "../helpers";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { ScreenError } from "../ScreenError";
import { text } from "../text";

/** `/access/grants/new`: give a person a role, with an optional end date. The person may come
 *  prefilled from a person page (`?person=<id>`). Every role that can be granted is offered; a
 *  department role also asks for one of the active departments (`GET /api/departments`), which a
 *  department page can prefill (`?department=<id>`, choosing the `department-admin` role). */
export function GrantNew() {
  const t = useT();
  const titles = useTitles();
  const toast = useToast();
  const navigate = useNavigate();
  const client = useQueryClient();
  const { readOnly } = useReadOnly();
  const [params] = useSearchParams();
  const [person, setPerson] = useState(params.get("person") ?? "");
  const [picked, setPicked] = useState("");
  const departmentParam = params.get("department") ?? "";
  const [roleChoice, setRoleChoice] = useState<string | null>(null);
  const formRef = useRef<HTMLFormElement>(null);
  const act = useAction();
  const fieldErrors = useFieldErrors(act.error);

  const rolesQ = useQuery({
    queryKey: ["access", "roles"],
    queryFn: ({ signal }) => api.GET("/api/access/roles", { signal }),
  });
  const departmentsQ = useQuery({
    queryKey: ["departments", "list", { archived: false }],
    queryFn: ({ signal }) => api.GET("/api/departments/", { signal }),
  });
  const nameOf = useNames([person]);
  const loadError =
    (rolesQ.error instanceof ApiError ? rolesQ.error : null) ??
    (departmentsQ.error instanceof ApiError ? departmentsQ.error : null);
  const grantable = (rolesQ.data?.roles ?? []).filter((r) => !r.base);
  // Coming from a department page, the lead's role is the one to give.
  const suggested =
    departmentParam !== "" ? (grantable.find((r) => r.key === "department-admin")?.id ?? "") : "";
  const roleId = roleChoice ?? suggested;
  const chosenRole = grantable.find((r) => r.id === roleId);
  const needsDepartment = chosenRole?.scope === "department";
  const departments = departmentsQ.data?.departments ?? [];

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const data = new FormData(e.currentTarget);
    const role = text(data.get("role"));
    const department = text(data.get("department"));
    const endsAt = endOfDayInstant(text(data.get("ends_at")));
    void act.run(async () => {
      // An empty role is left out, so the server's `required` lands on the role select.
      await api.POST("/api/access/grants", {
        body: {
          person_id: person,
          ...(role ? { role_id: role } : {}),
          ...(needsDepartment && department ? { department_id: department } : {}),
          ...(endsAt ? { ends_at: endsAt } : {}),
        } as { person_id: string; role_id: string; department_id?: string; ends_at?: string },
      });
      void client.invalidateQueries({ queryKey: ["access", "grants"] });
      toast.show(t("core.access.new_grant.done"));
      void navigate(`/access/grants?person=${person}`, { replace: true });
    });
  };

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-2xl font-bold">{t("core.access.new_grant.title")}</h1>
      <div>
        <Link to="/access/grants" className="inline-flex min-h-11 items-center underline">
          {t("core.access.new_grant.back")}
        </Link>
      </div>
      {rolesQ.isPending || departmentsQ.isPending ? <Loading /> : null}
      {loadError ? (
        <ScreenError
          error={loadError}
          onRetry={() => {
            void rolesQ.refetch();
            void departmentsQ.refetch();
          }}
        />
      ) : null}
      {rolesQ.data && departmentsQ.data ? (
        <form ref={formRef} onSubmit={submit} className="flex flex-col gap-5" noValidate>
          <ErrorSummary
            error={act.error}
            formRef={formRef}
            {...(act.error?.fields.some((f) => f.field === "actor")
              ? { message: t("core.access.new_grant.no_profile") }
              : {})}
          />
          <PersonSearch
            search={searchPeople}
            name="person"
            label={t("core.access.new_grant.person")}
            chosenName={picked || nameOf(person)}
            hasValue={person !== ""}
            onPick={(m) => {
              setPerson(m.id);
              setPicked(m.playa_name);
            }}
            onClear={() => setPerson("")}
            error={fieldErrors["person"]}
          />
          <Field
            label={t("core.access.new_grant.role")}
            name="role"
            required
            error={fieldErrors["role"]}
          >
            {(control) => (
              <select
                {...control}
                className={controlClass}
                value={roleId}
                onChange={(e) => setRoleChoice(e.target.value)}
              >
                <option value="" disabled>
                  {t("core.access.new_grant.choose_role")}
                </option>
                {grantable.map((r) => (
                  <option key={r.id} value={r.id}>
                    {titles.role(r)}
                  </option>
                ))}
              </select>
            )}
          </Field>
          {needsDepartment ? (
            <Field
              label={t("core.access.new_grant.department")}
              name="department"
              required
              error={fieldErrors["department"]}
            >
              {(control) => (
                <select {...control} className={controlClass} defaultValue={departmentParam}>
                  <option value="" disabled>
                    {t("core.access.new_grant.choose_department")}
                  </option>
                  {departments.map((d) => (
                    <option key={d.id} value={d.id}>
                      {d.name}
                    </option>
                  ))}
                </select>
              )}
            </Field>
          ) : null}
          <DateField
            label={t("core.access.new_grant.ends")}
            name="ends_at"
            hint={t("core.access.new_grant.ends_hint")}
            min={todayDate()}
            error={fieldErrors["ends_at"]}
          />
          <ReadOnlyNote id="grant-readonly" readOnly={readOnly} />
          {person === "" ? (
            <p id="grant-person-hint" className="text-sm text-muted">
              {t("core.access.new_grant.person_first")}
            </p>
          ) : null}
          <div>
            <Button
              type="submit"
              busy={act.busy}
              disabled={readOnly || person === ""}
              {...(readOnly || person === ""
                ? { "aria-describedby": readOnly ? "grant-readonly" : "grant-person-hint" }
                : {})}
            >
              {t("core.access.new_grant.submit")}
            </Button>
          </div>
        </form>
      ) : null}
    </div>
  );
}
