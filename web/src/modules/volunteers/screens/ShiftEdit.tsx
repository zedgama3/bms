import { useQuery } from "@tanstack/react-query";
import { Link, useLocation, useParams } from "react-router";
import { ApiError, useSession } from "@/api";
import { useT } from "@/i18n";
import { Loading } from "@/ui";
import { carry } from "../back";
import { vol } from "../calls";
import { Forbidden } from "../Forbidden";
import { holdsAt } from "../helpers";
import { P } from "../permissions";
import { ScreenError } from "../ScreenError";
import { ShiftFormBody } from "../ShiftFormBody";

/** `/volunteers/shifts/:shift/edit` (`shifts.manage` at the shift's department). */
export function ShiftEdit() {
  const t = useT();
  const session = useSession();
  const { shift: id = "" } = useParams();
  const location = useLocation();
  const q = useQuery({
    queryKey: ["volunteers", "shift", id],
    queryFn: ({ signal }) => vol.shift(id, { signal }),
  });
  const error = q.error instanceof ApiError ? q.error : null;
  if (q.data && !holdsAt(session, P.shiftsManage, q.data.department))
    return <Forbidden title={t("module.volunteers.form.edit_title")} />;
  return (
    <div className="flex flex-col gap-4" aria-busy={q.isPending || undefined}>
      <h1 className="text-2xl font-bold">{t("module.volunteers.form.edit_title")}</h1>
      <div>
        <Link
          to={`/volunteers/shifts/${id}`}
          state={carry(location)}
          className="inline-flex min-h-11 items-center underline"
        >
          {t("module.volunteers.shift.back_to")}
        </Link>
      </div>
      {q.isPending ? <Loading /> : null}
      {error ? <ScreenError error={error} onRetry={() => void q.refetch()} /> : null}
      {q.data ? (
        <ShiftFormBody key={q.data.id} department={q.data.department} shift={q.data} />
      ) : null}
    </div>
  );
}
