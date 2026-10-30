import { useEffect } from "react";
import { Link, useLocation, useNavigate, useParams, useRouteError } from "react-router";
import { ApiError } from "@/api";
import { useT } from "@/i18n";
import { Button, ErrorState, linkButtonClass } from "@/ui";
import { patternOf } from "./boot";
import { getReporter, isChunkError } from "./report";

/** The route's `errorElement`: the crashed-screen state (the frame stays), reported once. */
export function CrashedScreen() {
  const t = useT();
  const error = useRouteError();
  const location = useLocation();
  const params = useParams();
  const navigate = useNavigate();
  useEffect(() => {
    getReporter()?.screenCrash(error, patternOf(location.pathname, params));
    // one report per crash
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [error]);
  // A lazy chunk that no longer exists: the site was updated while this tab was open (F45).
  if (isChunkError(error)) {
    return (
      <section className="flex flex-col items-start gap-4" aria-labelledby="crashed-title">
        <h1 id="crashed-title" className="text-2xl font-bold">
          {t("shell.crashed.stale.title")}
        </h1>
        <p role="alert">{t("shell.crashed.stale.body")}</p>
        <Button onClick={() => window.location.reload()}>{t("shell.crashed.stale.reload")}</Button>
      </section>
    );
  }
  return (
    <section className="flex flex-col items-start gap-4" aria-labelledby="crashed-title">
      <h1 id="crashed-title" className="text-2xl font-bold">
        {t("shell.crashed.title")}
      </h1>
      {error instanceof ApiError ? (
        <ErrorState error={error} />
      ) : (
        <p role="alert">{t("shell.crashed.body")}</p>
      )}
      <div className="flex flex-wrap gap-3">
        <Button onClick={() => void navigate(location, { replace: true })}>
          {t("shell.crashed.retry")}
        </Button>
        <Link to="/" className={linkButtonClass("secondary")}>
          {t("shell.crashed.home")}
        </Link>
      </div>
    </section>
  );
}
