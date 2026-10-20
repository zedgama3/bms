import { Navigate } from "react-router";
import { useSession } from "@/api";
import { useT } from "@/i18n";
import { Forbidden } from "../Forbidden";
import { holdsSite } from "../helpers";

/** `/notify`: the templates for a templates manager, else the outbox, else the forbidden state. */
export function Index() {
  const t = useT();
  const session = useSession();
  if (holdsSite(session, "notify.templates.manage")) return <Navigate to="templates" replace />;
  if (holdsSite(session, "notify.outbox.manage")) return <Navigate to="outbox" replace />;
  return <Forbidden title={t("core.notify.templates.title")} />;
}
