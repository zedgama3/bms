import { Link, Navigate, useParams } from "react-router";
import { ApiError } from "@/api";
import { useT } from "@/i18n";
import { Banner, EmptyState, Loading } from "@/ui";
import { Forbidden } from "../Forbidden";
import { ScreenError } from "../ScreenError";
import { SettingsForm } from "../SettingsForm";
import { useSettingsDocument } from "../queries";
import { useGate } from "../useGate";
import { useSettingsText } from "../useSettingsText";

/** `/settings/:area` (`settings.read`): one area's settings as a form. The regional's own
 *  values are on `/settings`; an unknown area is not found. */
export function Area() {
  const t = useT();
  const text = useSettingsText();
  const { area: name = "" } = useParams();
  const { canRead, canManage } = useGate();
  const q = useSettingsDocument(canRead);

  if (name === "regional") return <Navigate to="/settings" replace />;
  if (!canRead) {
    return canManage ? (
      <div className="flex flex-col gap-4">
        <h1 className="text-2xl font-bold">{t("core.settings.title")}</h1>
        <Banner tone="warn" live={false}>
          {t("core.settings.manage_only")}
        </Banner>
      </div>
    ) : (
      <Forbidden title={t("core.settings.title")} />
    );
  }

  const error = q.error instanceof ApiError ? q.error : null;
  const area = q.data?.areas.find((a) => a.name === name);
  const title = area ? text.areaTitle(area) : t("core.settings.title");

  return (
    <div className="flex flex-col gap-4" aria-busy={q.isPending || undefined}>
      <h1 className="text-2xl font-bold">{title}</h1>
      <div>
        <Link to="/settings" className="inline-flex min-h-11 items-center underline">
          {t("core.settings.back")}
        </Link>
      </div>
      {q.isPending ? <Loading /> : null}
      {error ? <ScreenError error={error} onRetry={() => void q.refetch()} /> : null}
      {q.data && !area ? <EmptyState>{t("core.settings.not_found")}</EmptyState> : null}
      {area ? (
        <SettingsForm
          key={area.name}
          area={area}
          settings={area.settings}
          canManage={canManage}
          legend={title}
        />
      ) : null}
    </div>
  );
}
