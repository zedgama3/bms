import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";
import { ApiError, useSession } from "@/api";
import { useT } from "@/i18n";
import { EmptyState, linkButtonClass, Loading } from "@/ui";
import { vol } from "../calls";
import { holdsSite, ledDepartments } from "../helpers";
import { Forbidden } from "../Forbidden";
import { LEAD_PERMISSIONS, P } from "../permissions";
import { useDepartments } from "../queries";
import { ScreenError } from "../ScreenError";

/** `/volunteers/admin`: the departments the person leads (from the session's `p@D` entries and
 *  the department list), the waiting certificates and the org-wide catalogue. */
export function Admin() {
  const t = useT();
  const session = useSession();
  const led = ledDepartments(session, LEAD_PERMISSIONS);
  const canVerify = ledDepartments(session, [P.qualificationsVerify]);
  const canCatalogue = holdsSite(session, P.qualificationsManage);
  const departments = useDepartments();
  const pending = useQuery({
    queryKey: ["volunteers", "pending", "all"],
    enabled: canVerify.site || canVerify.ids.length > 0,
    queryFn: ({ signal }) => vol.pending(undefined, { signal }),
  });

  if (!led.site && led.ids.length === 0)
    return <Forbidden title={t("module.volunteers.admin.title")} />;

  const mine = (departments.data ?? []).filter((d) => led.site || led.ids.includes(d.id));
  const error = departments.error instanceof ApiError ? departments.error : null;
  const pendingError = pending.error instanceof ApiError ? pending.error : null;

  return (
    <div className="flex flex-col gap-5" aria-busy={departments.isPending || undefined}>
      <h1 className="text-2xl font-bold">{t("module.volunteers.admin.title")}</h1>
      {departments.isPending ? <Loading /> : null}
      {error ? <ScreenError error={error} onRetry={() => void departments.refetch()} /> : null}
      {pendingError ? (
        <ScreenError error={pendingError} onRetry={() => void pending.refetch()} />
      ) : null}
      {pending.data ? (
        <p role="status" className="font-semibold">
          {t("module.volunteers.admin.pending", { count: pending.data.length })}
        </p>
      ) : null}
      {departments.data && mine.length === 0 ? (
        <EmptyState>{t("module.volunteers.admin.none")}</EmptyState>
      ) : null}
      {mine.length > 0 ? (
        <section aria-labelledby="admin-departments" className="flex flex-col gap-3">
          <h2 id="admin-departments" className="text-xl font-bold">
            {t("module.volunteers.admin.departments")}
          </h2>
          <ul className="flex flex-col gap-2">
            {mine.map((d) => (
              <li key={d.id}>
                <Link
                  to={`/volunteers/departments/${d.id}`}
                  className="flex min-h-11 items-center rounded-md border border-border bg-surface px-3 py-2 font-semibold underline"
                >
                  {d.name}
                </Link>
              </li>
            ))}
          </ul>
        </section>
      ) : null}
      {canCatalogue ? (
        <div>
          <Link to="/volunteers/qualifications" className={linkButtonClass("secondary")}>
            {t("module.volunteers.admin.catalogue")}
          </Link>
        </div>
      ) : null}
    </div>
  );
}
