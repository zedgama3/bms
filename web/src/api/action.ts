import { useCallback, useRef, useState } from "react";
import { ApiError, isAbort } from "./errors";

/** One user action that calls the API: `run` sets busy, clears the last error, and keeps an
 *  ApiError for ErrorSummary or ErrorState. Resolves true on success. Nothing is cached, so a
 *  secret in the result (a temporary password) lives only where the caller puts it. `mapError`
 *  (a stable function) reshapes an error before it is kept (a module's own field codes). */
export function useAction(mapError?: (e: ApiError) => ApiError) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);
  const live = useRef(false);
  const run = useCallback(
    async (fn: () => Promise<void>): Promise<boolean> => {
      if (live.current) return false;
      live.current = true;
      setBusy(true);
      setError(null);
      try {
        await fn();
        return true;
      } catch (e) {
        if (isAbort(e)) return false;
        if (e instanceof ApiError) setError(mapError ? mapError(e) : e);
        else throw e;
        return false;
      } finally {
        live.current = false;
        setBusy(false);
      }
    },
    [mapError],
  );
  const clear = useCallback(() => setError(null), []);
  return { busy, error, run, clear };
}
