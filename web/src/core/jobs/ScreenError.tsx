import { ApiError } from "@/api";
import { useT } from "@/i18n";
import { ErrorState } from "@/ui";

/** What a direct visit without the permission shows: the same block a 403 on the read shows, but
 *  with no reference, because no request was made. */
const NOT_ASKED = new ApiError({
  status: 403,
  code: "permission",
  method: "GET",
  route: "/api/jobs/",
  module: "jobs",
  traceId: "",
});

/** A screen's failed read, or a direct visit without the permission (`error` absent): the
 *  forbidden sentence for a 403 (no retry can help; the reference when the server sent one), else
 *  the error for its code, with a retry. Every failure shows something (web-shell "Handle every
 *  state"). */
export function ScreenError({ error, onRetry }: { error?: ApiError; onRetry?: () => void }) {
  const t = useT();
  const e = error ?? NOT_ASKED;
  if (e.code === "permission") return <ErrorState error={e} message={t("shell.forbidden")} />;
  return <ErrorState error={e} {...(onRetry ? { onRetry } : {})} />;
}
