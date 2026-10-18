import { useQuery } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router";
import { api, ApiError, useReadOnly, useSession } from "@/api";
import { useT } from "@/i18n";
import { Button, Check, EmptyState, linkButtonClass, Loading } from "@/ui";
import { firstLine, holdsSite } from "../helpers";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { ScreenError } from "../ScreenError";

/** `/departments`: every active department as a card (name, first line of the description, the
 *  shared mailbox). Holders of `departments.manage` also get "New" and the archived toggle
 *  (`?archived=1`). */
export function DepartmentList() {
  const t = useT();
  const session = useSession();
  const { readOnly } = useReadOnly();
  const [params, setParams] = useSearchParams();
  const canManage = holdsSite(session, "departments.manage");
  const archived = canManage && params.get("archived") === "1";
  const q = useQuery({
    queryKey: ["departments", "list", { archived }],
    queryFn: ({ signal }) =>
      api.GET("/api/departments/", {
        signal,
        params: { query: archived ? { include_archived: true } : {} },
      }),
  });
  const error = q.error instanceof ApiError ? q.error : null;
  const departments = q.data?.departments ?? [];

  return (
    <div className="flex flex-col gap-4" aria-busy={q.isPending || undefined}>
      <h1 className="text-2xl font-bold">{t("core.departments.list.title")}</h1>
      {canManage ? (
        readOnly ? (
          <div className="flex flex-col gap-1">
            <div>
              <Button disabled aria-describedby="departments-readonly">
                {t("core.departments.list.new")}
              </Button>
            </div>
            <ReadOnlyNote id="departments-readonly" readOnly />
          </div>
        ) : (
          <div>
            <Link to="/departments/new" className={linkButtonClass("primary")}>
              {t("core.departments.list.new")}
            </Link>
          </div>
        )
      ) : null}
      {canManage ? (
        <Check
          name="archived"
          checked={archived}
          onChange={(e) => setParams(e.target.checked ? { archived: "1" } : {}, { replace: true })}
          label={t("core.departments.list.show_archived")}
        />
      ) : null}
      {q.isPending ? <Loading /> : null}
      {error ? <ScreenError error={error} onRetry={() => void q.refetch()} /> : null}
      {q.data && departments.length === 0 ? (
        <EmptyState>
          {canManage ? t("core.departments.list.empty_steward") : t("core.departments.list.empty")}
        </EmptyState>
      ) : null}
      {departments.length > 0 ? (
        <ul className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {departments.map((d) => (
            <li key={d.id} className="rounded-md border border-border bg-surface p-4">
              <h2 className="text-lg font-bold">
                <Link
                  to={`/departments/${d.id}`}
                  className="inline-flex min-h-11 items-center break-words underline"
                >
                  {d.name}
                </Link>
              </h2>
              {d.archived ? (
                <p className="mt-1">
                  <span className="rounded-md border border-border px-2 py-1 text-sm font-semibold">
                    {t("core.departments.archived_badge")}
                  </span>
                </p>
              ) : null}
              {firstLine(d.description) ? (
                <p className="mt-1 line-clamp-2 break-words">{firstLine(d.description)}</p>
              ) : null}
              {d.contact_email ? (
                <p className="mt-1">
                  <a
                    href={`mailto:${d.contact_email}`}
                    className="inline-flex min-h-11 items-center break-all underline"
                  >
                    {d.contact_email}
                  </a>
                </p>
              ) : null}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}
