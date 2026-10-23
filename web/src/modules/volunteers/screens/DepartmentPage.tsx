import { Link, useParams, useSearchParams } from "react-router";
import { ApiError, useSession } from "@/api";
import { useT } from "@/i18n";
import type { MessageKey } from "@/i18n";
import { EmptyState, Loading } from "@/ui";
import { CatalogueEditor } from "../CatalogueEditor";
import { Forbidden } from "../Forbidden";
import { holdsAt } from "../helpers";
import { P } from "../permissions";
import { useDepartments } from "../queries";
import { ScreenError } from "../ScreenError";
import { MembersTab } from "../tabs/MembersTab";
import { PendingTab } from "../tabs/PendingTab";
import { PositionsTab } from "../tabs/PositionsTab";
import { ShiftsTab } from "../tabs/ShiftsTab";

type Tab = "shifts" | "positions" | "members" | "qualifications" | "pending";
const LABELS: Record<Tab, MessageKey> = {
  shifts: "module.volunteers.tab.shifts",
  positions: "module.volunteers.tab.positions",
  members: "module.volunteers.tab.members",
  qualifications: "module.volunteers.tab.qualifications",
  pending: "module.volunteers.tab.pending",
};

/** `/volunteers/departments/:department`: a lead's page, one tab at a time (`?tab=`). A tab the
 *  session does not show a permission for is not offered; the server still answers each call. */
export function DepartmentPage() {
  const t = useT();
  const session = useSession();
  const { department = "" } = useParams();
  const [params] = useSearchParams();
  const departments = useDepartments();
  const has = (p: string) => holdsAt(session, p, department);

  const visible: Tab[] = [
    ...(has(P.shiftsManage) || has(P.rostersRead) ? (["shifts"] as const) : []),
    ...(has(P.positionsManage) ? (["positions"] as const) : []),
    ...(has(P.membersManage) || has(P.rostersRead) ? (["members"] as const) : []),
    ...(has(P.qualificationsManage) || has(P.qualificationsRead) || has(P.qualificationsVerify)
      ? (["qualifications"] as const)
      : []),
    ...(has(P.qualificationsVerify) ? (["pending"] as const) : []),
  ];
  const asked = params.get("tab");
  const tab: Tab | undefined = visible.find((x) => x === asked) ?? visible[0];
  const dept = departments.data?.find((d) => d.id === department);
  const error = departments.error instanceof ApiError ? departments.error : null;
  const title = dept?.name ?? t("module.volunteers.department.title");

  if (!tab) return <Forbidden title={t("module.volunteers.department.title")} />;

  return (
    <div className="flex flex-col gap-4" aria-busy={departments.isPending || undefined}>
      <div>
        <Link to="/volunteers/admin" className="inline-flex min-h-11 items-center underline">
          {t("module.volunteers.department.back")}
        </Link>
      </div>
      <h1 className="text-2xl font-bold">{title}</h1>
      {departments.isPending ? <Loading /> : null}
      {error ? <ScreenError error={error} onRetry={() => void departments.refetch()} /> : null}
      {departments.data && !dept ? <EmptyState>{t("error.not_found")}</EmptyState> : null}
      {dept ? (
        <>
          <nav aria-label={t("module.volunteers.department.tabs")}>
            <ul className="flex flex-wrap gap-2">
              {visible.map((x) => (
                <li key={x}>
                  <Link
                    to={`?tab=${x}`}
                    replace
                    {...(x === tab ? { "aria-current": "page" as const } : {})}
                    className={`inline-flex min-h-11 items-center rounded-md border border-border px-4 font-semibold ${x === tab ? "bg-surface underline underline-offset-4" : ""}`}
                  >
                    {t(LABELS[x])}
                  </Link>
                </li>
              ))}
            </ul>
          </nav>
          <section aria-labelledby="tab-heading" className="flex flex-col gap-4">
            <h2 id="tab-heading" className="text-xl font-bold">
              {t(LABELS[tab])}
            </h2>
            {tab === "shifts" ? <ShiftsTab department={department} /> : null}
            {tab === "positions" ? <PositionsTab department={department} /> : null}
            {tab === "members" ? <MembersTab department={department} /> : null}
            {tab === "qualifications" ? (
              <CatalogueEditor department={department} canManage={has(P.qualificationsManage)} />
            ) : null}
            {tab === "pending" ? <PendingTab department={department} /> : null}
          </section>
        </>
      ) : null}
    </div>
  );
}
