import type { ApiError } from "@/api";
import { isMessageKey, useT } from "@/i18n";
import { ErrorState } from "@/ui";
import { useQualifications } from "./queries";

/** The sentence for a refused action: a conflict names its code (`full`, `overlap`, ...) when
 *  the catalogue has one, so the person learns why; else the shell's sentence for the code. */
export function useConflictMessage(error: ApiError | null): string | undefined {
  const t = useT();
  // Only a conflict names qualifications; an error shown for the catalogue's own failed read
  // must not mount an observer that refetches it again (a loop).
  const quals = useQualifications(error?.code === "conflict");
  if (!error || error.code !== "conflict") return undefined;
  for (const f of error.fields) {
    // `unqualified` names the missing qualification IDs (comma-separated): show their names
    const missing = f.code === "unqualified" ? (f.params?.["missing"] ?? "") : "";
    if (missing) {
      const names = missing
        .split(",")
        .map((id) => quals.data?.find((q) => q.id === id)?.name)
        .filter((n): n is string => Boolean(n));
      if (names.length > 0)
        return t("module.volunteers.field.unqualified_named", {
          names: names.join(", "),
          count: names.length,
        });
    }
    const key = `module.volunteers.field.${f.code}`;
    if (isMessageKey(key)) return t(key, f.params ?? {});
  }
  return undefined;
}

/** A screen's failed read or refused action: the forbidden sentence for a 403, the not-found
 *  sentence for an unknown or malformed ID, else the error for its code, with a retry for a
 *  read. Every failure shows something (web-shell "Handle every state"). */
export function ScreenError({ error, onRetry }: { error: ApiError; onRetry?: () => void }) {
  const t = useT();
  const conflict = useConflictMessage(error);
  if (error.code === "permission")
    return <ErrorState error={error} message={t("shell.forbidden")} />;
  if (error.code === "invalid" || error.code === "not_found")
    return <ErrorState error={error} message={t("error.not_found")} />;
  return (
    <ErrorState
      error={error}
      {...(conflict ? { message: conflict } : {})}
      {...(onRetry && error.code !== "conflict" ? { onRetry } : {})}
    />
  );
}
