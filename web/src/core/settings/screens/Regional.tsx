import { Link } from "react-router";
import { ApiError } from "@/api";
import { useT } from "@/i18n";
import { Banner, EmptyState, linkButtonClass, Loading } from "@/ui";
import { Forbidden } from "../Forbidden";
import { regionalControls } from "../RegionalControls";
import { ScreenError } from "../ScreenError";
import { SettingsForm } from "../SettingsForm";
import { useSettingsDocument } from "../queries";
import { useGate } from "../useGate";
import { useSettingsText } from "../useSettingsText";

/** `/settings` (`settings.read`): the regional's own values in one form, then links to
 *  Terminology and to each other area's settings. */
export function Regional() {
  const t = useT();
  const text = useSettingsText();
  const { canRead, canManage } = useGate();
  const q = useSettingsDocument(canRead);

  if (!canRead) {
    // `manage` without `read` can save and not show: say so, with no form.
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
  const areas = q.data?.areas ?? [];
  const regional = areas.find((a) => a.name === "regional");
  const others = areas.filter((a) => a.name !== "regional");

  return (
    <div className="flex flex-col gap-6" aria-busy={q.isPending || undefined}>
      <h1 className="text-2xl font-bold">{t("core.settings.title")}</h1>
      {q.isPending && canRead ? <Loading /> : null}
      {error ? <ScreenError error={error} onRetry={() => void q.refetch()} /> : null}
      {q.data && !regional ? <EmptyState>{t("core.settings.not_found")}</EmptyState> : null}
      {regional ? (
        <section aria-labelledby="settings-regional" className="flex flex-col gap-4">
          <h2 id="settings-regional" className="text-xl font-bold">
            {t("core.settings.regional.heading")}
          </h2>
          <p>{t("core.settings.regional.intro")}</p>
          <SettingsForm
            area={regional}
            settings={regional.settings}
            canManage={canManage}
            controls={regionalControls}
            legend={t("core.settings.regional.heading")}
          />
        </section>
      ) : null}
      {q.data ? (
        <section aria-labelledby="settings-more" className="flex flex-col gap-3">
          <h2 id="settings-more" className="text-xl font-bold">
            {t("core.settings.more.heading")}
          </h2>
          <ul className="flex flex-col gap-3">
            <li>
              <Link to="/settings/terms" className={linkButtonClass("secondary")}>
                {t("core.settings.more.terms")}
              </Link>
              <p className="mt-1 text-sm text-muted">{t("core.settings.more.terms_hint")}</p>
            </li>
            {others.map((a) => (
              <li key={a.name}>
                <Link
                  to={`/settings/${encodeURIComponent(a.name)}`}
                  className={linkButtonClass("secondary")}
                >
                  {text.areaTitle(a)}
                </Link>
              </li>
            ))}
          </ul>
        </section>
      ) : null}
    </div>
  );
}
