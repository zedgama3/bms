import { useCallback, useRef, useState } from "react";
import { ApiError, isAbort } from "@/api";

/** One user action that calls the API: `run` sets busy, clears the last error, and keeps an
 *  ApiError for ErrorSummary or ErrorState. Resolves true on success. */
export function useAction() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);
  const live = useRef(false);
  const run = useCallback(async (fn: () => Promise<void>): Promise<boolean> => {
    if (live.current) return false;
    live.current = true;
    setBusy(true);
    setError(null);
    try {
      await fn();
      return true;
    } catch (e) {
      if (isAbort(e)) return false;
      if (e instanceof ApiError) setError(e);
      else throw e;
      return false;
    } finally {
      live.current = false;
      setBusy(false);
    }
  }, []);
  return { busy, error, run };
}
