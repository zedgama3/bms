import { useSession } from "@/api";
import { useT } from "@/i18n";
import { Link, useParams } from "react-router";
import { Forbidden } from "../Forbidden";
import { holdsAt } from "../helpers";
import { P } from "../permissions";
import { ShiftFormBody } from "../ShiftFormBody";

/** `/volunteers/departments/:department/shifts/new` (`shifts.manage` at the department). */
export function ShiftNew() {
  const t = useT();
  const session = useSession();
  const { department = "" } = useParams();
  if (!holdsAt(session, P.shiftsManage, department))
    return <Forbidden title={t("module.volunteers.form.new_title")} />;
  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-2xl font-bold">{t("module.volunteers.form.new_title")}</h1>
      <div>
        <Link
          to={`/volunteers/departments/${department}`}
          className="inline-flex min-h-11 items-center underline"
        >
          {t("module.volunteers.form.back")}
        </Link>
      </div>
      <ShiftFormBody department={department} />
    </div>
  );
}
