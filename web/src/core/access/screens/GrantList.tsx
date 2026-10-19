import { useRef, useState } from "react";
import type { FormEvent } from "react";
import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router";
import { api, ApiError, useReadOnly, useSession } from "@/api";
import { useFormat, useT } from "@/i18n";
import {
  Button,
  Check,
  controlClass,
  Dialog,
  EmptyState,
  ErrorSummary,
  Field,
  lastDayOf,
  linkButtonClass,
  Loading,
  useToast,
} from "@/ui";
import { focusSoon } from "../focus";
import { holds, useTitles } from "../helpers";
import { PersonPicker } from "../PersonPicker";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { ScreenError } from "../ScreenError";
import type { Grant } from "../types";
import { useAction } from "../useAction";
import { useNames } from "../useNames";

/** `/access/grants`: who holds which role. Filters live in the query string as IDs only
 *  (`person`, `role`, `department`, `permission`, `include_ended`); the name typed to find a person stays in
 *  the picker. */
export function GrantList() {
  const t = useT();
  const format = useFormat();
  const titles = useTitles();
  const toast = useToast();
  const client = useQueryClient();
  const session = useSession();
  const { readOnly } = useReadOnly();
  const [params, setParams] = useSearchParams();
  // Without grants-read the screen is the person's own roles: no filters, no picker.
  const canRead = holds(session, "access.grants.read");
  const selfOnly = !canRead && session.signedIn;
  const person = selfOnly && session.signedIn ? session.personId : (params.get("person") ?? "");
  // `role` is a role ID or a starter role's key (the server accepts both).
  const role = params.get("role") ?? "";
  const department = params.get("department") ?? "";
  const permission = params.get("permission") ?? "";
  const includeEnded = params.get("include_ended") === "true";
  const [revoking, setRevoking] = useState<Grant | null>(null);
  const revokeRef = useRef<HTMLFormElement>(null);
  const act = useAction();

  const setParam = (key: string, value: string) =>
    setParams(
      (cur) => {
        const next = new URLSearchParams(cur);
        if (value) next.set(key, value);
        else next.delete(key);
        return next;
      },
      { replace: true },
    );

  const rolesQ = useQuery({
    queryKey: ["access", "roles"],
    enabled: !selfOnly,
    queryFn: ({ signal }) => api.GET("/api/access/roles", { signal }),
  });
  // The select's options are role IDs: a key-valued filter selects the starter role with that key.
  const roleOptionValue =
    (rolesQ.data?.roles ?? []).find((r) => r.id === role || (r.key && r.key === role))?.id ?? role;
  const catQ = useQuery({
    queryKey: ["access", "permissions"],
    enabled: !selfOnly,
    queryFn: ({ signal }) => api.GET("/api/access/permissions", { signal }),
  });

  const departmentsQ = useQuery({
    queryKey: ["departments", "list", { archived: false }],
    queryFn: ({ signal }) => api.GET("/api/departments/", { signal }),
  });

  const q = useInfiniteQuery({
    queryKey: ["access", "grants", { person, role, department, permission, includeEnded }],
    initialPageParam: "",
    queryFn: ({ signal, pageParam }) =>
      api.GET("/api/access/grants", {
        signal,
        params: {
          query: {
            ...(person ? { person } : {}),
            ...(role ? { role } : {}),
            ...(department ? { department } : {}),
            ...(permission ? { permission } : {}),
            ...(includeEnded ? { include_ended: true } : {}),
            ...(pageParam ? { after: pageParam } : {}),
          },
        },
      }),
    getNextPageParam: (last) => last.next || undefined,
  });

  const grants = q.data?.pages.flatMap((p) => p.grants) ?? [];
  const nameOf = useNames([person, ...grants.flatMap((g) => [g.person_id, g.granted_by ?? ""])]);
  // The server names each grant's role: its name, or the starter role's key (never the ID).
  const roleTitle = (g: Grant) => titles.role({ name: g.role_name, key: g.role_key });
  const departments = departmentsQ.data?.departments ?? [];
  // A department that is not among the active ones is archived, once the list has arrived.
  const departmentTitle = (id: string) =>
    departments.find((d) => d.id === id)?.name ??
    (departmentsQ.data
      ? t("core.access.grants.department_archived")
      : t("core.access.grants.department_unknown"));
  const loadError =
    (rolesQ.error instanceof ApiError ? rolesQ.error : null) ??
    (catQ.error instanceof ApiError ? catQ.error : null) ??
    (!selfOnly && departmentsQ.error instanceof ApiError ? departmentsQ.error : null);
  const error = q.error instanceof ApiError ? q.error : null;
  const canManage = !selfOnly && holds(session, "access.grants.manage");

  const revoke = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const g = revoking;
    if (!g) return;
    void act.run(async () => {
      await api.DELETE("/api/access/grants/{grant}", { params: { path: { grant: g.id } } });
      toast.show(t("core.access.grants.revoked"));
      setRevoking(null);
      void client.invalidateQueries({ queryKey: ["access", "grants"] });
      focusSoon("main h1");
    });
  };

  return (
    <div className="flex flex-col gap-4" aria-busy={q.isPending || undefined}>
      <h1 className="text-2xl font-bold">
        {selfOnly ? t("core.access.grants.self_title") : t("core.access.grants.title")}
      </h1>

      {canManage ? (
        readOnly ? (
          <div className="flex flex-col gap-1">
            <div>
              <Button disabled aria-describedby="grants-readonly">
                {t("core.access.grants.grant")}
              </Button>
            </div>
            <ReadOnlyNote id="grants-readonly" readOnly />
          </div>
        ) : (
          <div>
            <Link
              to={person ? `/access/grants/new?person=${person}` : "/access/grants/new"}
              className={linkButtonClass("primary")}
            >
              {t("core.access.grants.grant")}
            </Link>
          </div>
        )
      ) : null}

      {selfOnly ? null : (
        <section aria-labelledby="grants-filters" className="flex flex-col gap-3">
          <h2 id="grants-filters" className="text-lg font-bold">
            {t("core.access.grants.filters")}
          </h2>
          <PersonPicker
            name="person"
            label={t("core.access.grants.person")}
            chosenName={nameOf(person)}
            hasValue={person !== ""}
            onPick={(m) => setParam("person", m.id)}
            onClear={() => setParam("person", "")}
          />
          <Field label={t("core.access.grants.role")} name="role">
            {(control) => (
              <select
                {...control}
                className={controlClass}
                value={roleOptionValue}
                onChange={(e) => setParam("role", e.target.value)}
              >
                <option value="">{t("core.access.grants.any_role")}</option>
                {(rolesQ.data?.roles ?? []).map((r) => (
                  <option key={r.id} value={r.id}>
                    {titles.role(r)}
                  </option>
                ))}
              </select>
            )}
          </Field>
          <Field label={t("core.access.grants.department_filter")} name="department">
            {(control) => (
              <select
                {...control}
                className={controlClass}
                value={department}
                onChange={(e) => setParam("department", e.target.value)}
              >
                <option value="">{t("core.access.grants.any_department")}</option>
                {departments.map((d) => (
                  <option key={d.id} value={d.id}>
                    {d.name}
                  </option>
                ))}
              </select>
            )}
          </Field>
          <Field
            label={t("core.access.grants.permission")}
            name="permission"
            hint={t("core.access.grants.permission_hint")}
          >
            {(control) => (
              <select
                {...control}
                className={controlClass}
                value={permission}
                onChange={(e) => setParam("permission", e.target.value)}
              >
                <option value="">{t("core.access.grants.any_permission")}</option>
                {(catQ.data?.permissions ?? []).map((p) => (
                  <option key={p.name} value={p.name}>
                    {titles.permission(p.name)}
                  </option>
                ))}
              </select>
            )}
          </Field>
          <Check
            name="include_ended"
            checked={includeEnded}
            onChange={(e) => setParam("include_ended", e.target.checked ? "true" : "")}
            label={t("core.access.grants.include_ended")}
          />
        </section>
      )}
      {loadError ? (
        <ScreenError
          error={loadError}
          onRetry={() => {
            void rolesQ.refetch();
            void catQ.refetch();
            void departmentsQ.refetch();
          }}
        />
      ) : null}

      {q.isPending ? <Loading /> : null}
      {error && grants.length === 0 ? (
        <ScreenError error={error} onRetry={() => void q.refetch()} />
      ) : null}
      {q.data && grants.length === 0 ? (
        <EmptyState>
          {selfOnly ? t("core.access.grants.self_empty") : t("core.access.grants.empty")}
        </EmptyState>
      ) : null}
      {grants.length > 0 ? (
        <ul className="flex flex-col gap-3" aria-label={t("core.access.grants.list")}>
          {grants.map((g) => (
            <li key={g.id} className="rounded-md border border-border bg-surface p-4">
              <h2 className="break-words text-lg font-bold">{nameOf(g.person_id)}</h2>
              <p className="font-semibold">{roleTitle(g)}</p>
              {g.department_id ? (
                <p>
                  {t("core.access.grants.department", {
                    department: departmentTitle(g.department_id),
                  })}
                </p>
              ) : null}
              <p>
                {g.ended
                  ? t("core.access.grants.ended", {
                      date: g.ends_at ? format.date(lastDayOf(g.ends_at)) : "",
                    })
                  : g.ends_at
                    ? t("core.access.grants.until", { date: format.date(lastDayOf(g.ends_at)) })
                    : t("core.access.grants.no_end")}
              </p>
              <p className="text-sm text-muted">
                {g.granted_by
                  ? t("core.access.grants.granted_by", {
                      date: format.date(g.created_at),
                      name: nameOf(g.granted_by),
                    })
                  : t("core.access.grants.granted", { date: format.date(g.created_at) })}
              </p>
              {canManage ? (
                <div className="mt-2">
                  <Button
                    variant="danger"
                    disabled={readOnly}
                    {...(readOnly ? { "aria-describedby": "grants-readonly" } : {})}
                    onClick={() => {
                      act.clear();
                      setRevoking(g);
                    }}
                  >
                    {t("core.access.grants.revoke")}
                  </Button>
                </div>
              ) : null}
            </li>
          ))}
        </ul>
      ) : null}
      {q.hasNextPage ? (
        <div>
          <Button
            variant="secondary"
            busy={q.isFetchingNextPage}
            onClick={() => void q.fetchNextPage()}
          >
            {t("core.access.grants.more")}
          </Button>
        </div>
      ) : null}

      <Dialog
        open={revoking !== null}
        title={t("core.access.grants.revoke_title")}
        onClose={() => setRevoking(null)}
      >
        <form ref={revokeRef} onSubmit={revoke} className="flex flex-col gap-4">
          <ErrorSummary error={act.error} formRef={revokeRef} />
          <p>
            {revoking
              ? t("core.access.grants.revoke_body", {
                  name: nameOf(revoking.person_id),
                  role: roleTitle(revoking),
                })
              : ""}
          </p>
          <div className="flex flex-wrap gap-3">
            <Button type="submit" variant="danger" busy={act.busy}>
              {t("core.access.grants.revoke_confirm")}
            </Button>
            <Button variant="secondary" onClick={() => setRevoking(null)}>
              {t("core.access.cancel")}
            </Button>
          </div>
        </form>
      </Dialog>
    </div>
  );
}
