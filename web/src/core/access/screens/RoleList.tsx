import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";
import { api, ApiError, useReadOnly, useSession } from "@/api";
import { useT } from "@/i18n";
import { Button, EmptyState, linkButtonClass, Loading } from "@/ui";
import { holds, useTitles } from "../helpers";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { ScreenError } from "../ScreenError";
import { Tabs } from "../Tabs";

/** `/access/roles`: every role as a card (name or starter title, scope, "everyone" for the base
 *  role, how many permissions it holds). */
export function RoleList() {
  const t = useT();
  const titles = useTitles();
  const session = useSession();
  const { readOnly } = useReadOnly();
  const q = useQuery({
    queryKey: ["access", "roles"],
    queryFn: ({ signal }) => api.GET("/api/access/roles", { signal }),
  });
  const error = q.error instanceof ApiError ? q.error : null;
  const canManage = holds(session, "access.roles.manage");
  const roles = q.data?.roles ?? [];

  return (
    <div className="flex flex-col gap-4" aria-busy={q.isPending || undefined}>
      <h1 className="text-2xl font-bold">{t("core.access.roles.title")}</h1>
      <Tabs showRules={holds(session, "access.roles.read")} />
      {canManage ? (
        readOnly ? (
          <div className="flex flex-col gap-1">
            <div>
              <Button disabled aria-describedby="roles-readonly">
                {t("core.access.roles.new")}
              </Button>
            </div>
            <ReadOnlyNote id="roles-readonly" readOnly />
          </div>
        ) : (
          <div>
            <Link to="/access/roles/new" className={linkButtonClass("primary")}>
              {t("core.access.roles.new")}
            </Link>
          </div>
        )
      ) : null}
      {q.isPending ? <Loading /> : null}
      {error ? <ScreenError error={error} onRetry={() => void q.refetch()} /> : null}
      {q.data && roles.length === 0 ? (
        <EmptyState>{t("core.access.roles.empty")}</EmptyState>
      ) : null}
      {roles.length > 0 ? (
        <ul className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {roles.map((r) => (
            <li key={r.id} className="rounded-md border border-border bg-surface p-4">
              <h2 className="text-lg font-bold">
                <Link
                  to={`/access/roles/${r.id}`}
                  className="inline-flex min-h-11 items-center break-words underline"
                >
                  {titles.role(r)}
                </Link>
              </h2>
              <ul className="mt-1 flex flex-wrap gap-2 text-sm">
                <li className="rounded-md border border-border px-2 py-1">
                  {titles.scope(r.scope)}
                </li>
                {r.base ? (
                  <li className="rounded-md border border-border px-2 py-1 font-semibold">
                    {t("core.access.roles.everyone")}
                  </li>
                ) : null}
              </ul>
              <p className="mt-2 text-muted">
                {t("core.access.roles.count", { count: r.permissions.length })}
              </p>
              {r.description ? <p className="mt-1 break-words">{r.description}</p> : null}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}
