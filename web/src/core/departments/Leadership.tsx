import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";
import { api, ApiError } from "@/api";
import type { components } from "@/api";
import { useT } from "@/i18n";
import { linkButtonClass, Loading } from "@/ui";
import { ScreenError } from "./ScreenError";
import type { Department } from "./types";

/** The Leadership card (departments-events.md decision 19). Someone who may read grants (site-
 *  wide or at this department) sees who holds `department-admin` here, with links to the grant
 *  screens; anyone else sees the shared mailbox only. Which grants come back is the server's
 *  call: this only decides what to ask. */
export function Leadership({
  department,
  canRead,
  canAppoint,
}: {
  department: Department;
  canRead: boolean;
  canAppoint: boolean;
}) {
  const t = useT();
  return (
    <section
      aria-labelledby="department-leadership"
      className="flex flex-col gap-3 rounded-md border border-border bg-surface p-4"
    >
      <h2 id="department-leadership" className="text-lg font-bold">
        {t("core.departments.leadership.title")}
      </h2>
      {canRead ? (
        <Leads department={department} canAppoint={canAppoint} />
      ) : department.contact_email ? (
        <p>
          {t("core.departments.leadership.write_to")}{" "}
          <a
            href={`mailto:${department.contact_email}`}
            className="inline-flex min-h-11 items-center break-all underline"
          >
            {department.contact_email}
          </a>
        </p>
      ) : (
        <p>{t("core.departments.leadership.no_mailbox")}</p>
      )}
    </section>
  );
}

const MAX_PAGES = 10;
type Grant = components["schemas"]["AccessGrant"];

function Leads({ department, canAppoint }: { department: Department; canAppoint: boolean }) {
  const t = useT();
  const grantsQ = useQuery({
    queryKey: ["departments", "leads", department.id],
    // Every page is read, so a department with many grants never wrongly shows no leads.
    queryFn: async ({ signal }) => {
      const grants: Grant[] = [];
      let truncated = false;
      let after = "";
      for (let page = 0; page < MAX_PAGES; page++) {
        const out = await api.GET("/api/access/grants", {
          signal,
          params: { query: { department: department.id, ...(after ? { after } : {}) } },
        });
        grants.push(...out.grants);
        if (!out.next) break;
        after = out.next;
        truncated = page === MAX_PAGES - 1;
      }
      return { grants, truncated };
    },
  });
  const leads = (grantsQ.data?.grants ?? []).filter((g) => g.role_key === "department-admin");
  const ids = [...new Set(leads.map((g) => g.person_id))].sort().slice(0, 200);
  const namesQ = useQuery({
    queryKey: ["departments", "names", ids.join(",")],
    enabled: ids.length > 0,
    queryFn: ({ signal }) =>
      api.GET("/api/people/names", { signal, params: { query: { ids: ids.join(",") } } }),
  });
  const names = namesQ.data?.names ?? {};
  const error = grantsQ.error instanceof ApiError ? grantsQ.error : null;
  const namesError = namesQ.error instanceof ApiError ? namesQ.error : null;
  return (
    <>
      {grantsQ.isPending ? <Loading /> : null}
      {error ? <ScreenError error={error} onRetry={() => void grantsQ.refetch()} /> : null}
      {namesError ? <ScreenError error={namesError} onRetry={() => void namesQ.refetch()} /> : null}
      {grantsQ.data?.truncated ? (
        <p className="text-muted">{t("core.departments.leadership.truncated")}</p>
      ) : null}
      {grantsQ.data && leads.length === 0 ? <p>{t("core.departments.leadership.none")}</p> : null}
      {leads.length > 0 ? (
        <ul className="flex flex-col gap-1" aria-label={t("core.departments.leadership.list")}>
          {leads.map((g) => (
            <li key={g.id} className="break-words font-semibold">
              {names[g.person_id] || t("core.departments.leadership.unknown_person")}
            </li>
          ))}
        </ul>
      ) : null}
      <div className="flex flex-wrap gap-3">
        <Link
          to={`/access/grants?department=${department.id}`}
          className={linkButtonClass("secondary")}
        >
          {t("core.departments.leadership.see_grants")}
        </Link>
        {canAppoint && !department.archived ? (
          <Link
            to={`/access/grants/new?department=${department.id}`}
            className={linkButtonClass("primary")}
          >
            {t("core.departments.leadership.appoint")}
          </Link>
        ) : null}
      </div>
    </>
  );
}
