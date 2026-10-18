import { useEffect } from "react";
import { TriangleAlert } from "lucide-react";
import type { ApiError } from "@/api";
import { useT } from "@/i18n";
import { Button } from "../Button";
import { isUntranslatedCode, messageFor, showsRef } from "./messages";
import { ReferenceLine } from "./ReferenceLine";
import { notifyUntranslated } from "./untranslated";

/** A screen's error state: the sentence, the reference line when `showsRef(error)`
 *  (decision 9), a retry button when `onRetry` is given. */
export function ErrorState({
  error,
  onRetry,
  message,
}: {
  error: ApiError;
  onRetry?: () => void;
  /** Replaces the sentence for the error's code (the accounts list says what it needs permission for). */
  message?: string;
}) {
  const t = useT();
  const untranslated = isUntranslatedCode(error);
  useEffect(() => {
    if (untranslated) notifyUntranslated("error", error.code, error);
  }, [untranslated, error]);
  return (
    <div
      role="alert"
      className="flex flex-col items-start gap-3 rounded-md border border-danger bg-danger-bg p-4"
    >
      <p className="flex items-start gap-2 font-semibold text-danger">
        <TriangleAlert aria-hidden="true" className="mt-1 size-5 shrink-0" />
        <span>{message ?? messageFor(t, error)}</span>
      </p>
      {showsRef(error) && error.ref ? <ReferenceLine reference={error.ref} /> : null}
      {onRetry ? (
        <Button variant="secondary" onClick={onRetry}>
          {t("shell.retry")}
        </Button>
      ) : null}
    </div>
  );
}
