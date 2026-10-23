import { useSession } from "@/api";
import { useT } from "@/i18n";
import { CatalogueEditor } from "../CatalogueEditor";
import { Forbidden } from "../Forbidden";
import { holdsSite } from "../helpers";
import { P } from "../permissions";

/** `/volunteers/qualifications`: the org-wide catalogue (site-wide `qualifications.manage`). */
export function OrgQualifications() {
  const t = useT();
  const session = useSession();
  if (!holdsSite(session, P.qualificationsManage))
    return <Forbidden title={t("module.volunteers.catalogue.title")} />;
  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-2xl font-bold">{t("module.volunteers.catalogue.title")}</h1>
      <p className="text-muted">{t("module.volunteers.catalogue.intro")}</p>
      <CatalogueEditor canManage />
    </div>
  );
}
