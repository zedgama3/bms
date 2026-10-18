import type { ApiError } from "@/api";
import { useT } from "@/i18n";
import { ErrorState } from "@/ui";

/** A screen's failed read: the forbidden sentence for a 403 (no retry can help), the shell's
 *  not-found sentence for an unknown or malformed ID in the path (a retry cannot help), else the error for its code, with a
 *  retry. Every failed read shows something (web-shell "Handle every state"). */
export function ScreenError({ error, onRetry }: { error: ApiError; onRetry: () => void }) {
  const t = useT();
  if (error.code === "permission")
    return <ErrorState error={error} message={t("shell.forbidden")} />;
  if (error.code === "invalid" || error.code === "not_found")
    return <ErrorState error={error} message={t("error.not_found")} />;
  return <ErrorState error={error} onRetry={onRetry} />;
}
