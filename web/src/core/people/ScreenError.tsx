import type { ApiError } from "@/api";
import { useT } from "@/i18n";
import { ErrorState } from "@/ui";

/** A screen's failed read: the forbidden sentence for a 403 (no retry can help), the shell's
 *  not-found sentence for a malformed ID in the path, else the error for its code, with a
 *  retry. Every failed read shows something (web-shell "Handle every state"). */
export function ScreenError({ error, onRetry }: { error: ApiError; onRetry: () => void }) {
  const t = useT();
  if (error.code === "permission")
    return <ErrorState error={error} message={t("shell.forbidden")} />;
  if (error.code === "invalid") return <ErrorState error={error} message={t("error.not_found")} />;
  return <ErrorState error={error} onRetry={onRetry} />;
}

/** The forbidden state shown up front, before any call, when the session lacks the permission. */
export function Forbidden() {
  const t = useT();
  return (
    <p
      role="alert"
      className="rounded-md border border-danger bg-danger-bg p-4 font-semibold text-danger"
    >
      {t("shell.forbidden")}
    </p>
  );
}
