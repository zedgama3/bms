import type { QueryClient } from "@tanstack/react-query";
import type { DataRouter } from "react-router";
import { configureClient } from "@/api/internal";
import type { Reporter } from "./report";
import { setReadOnly } from "./readonly";
import { isBootKey, SESSION_KEY } from "./bootKeys";
import type { SessionResult } from "./bootKeys";
import { resetSession, setSessionEnded, signedOutLike } from "./session";

const SIGN_IN = "/accounts/sign-in";

/** Installs the client's three hooks (decision 6): a 401 clears the session and the query
 *  cache and goes to `/accounts/sign-in?returnTo=`; X-Read-Only drives the banner; every failure goes to
 *  the reporter, which decides. */
export function wireClient(deps: {
  reporter: Pick<Reporter, "apiFailure">;
  queryClient: QueryClient;
  getRouter: () => DataRouter | null;
}): void {
  configureClient({
    onUnauthenticated(returnTo) {
      setSessionEnded(true);
      resetSession();
      // Drop what the old session fetched (up to sensitive records), but keep the boot
      // queries and mark the session signed out, so signing in again can recover.
      deps.queryClient.removeQueries({
        predicate: (q) => !isBootKey(q.queryKey),
      });
      deps.queryClient.setQueryData<SessionResult>(SESSION_KEY, (old) => ({
        identity: old?.identity ?? true,
        session: signedOutLike(old?.session ?? { signedIn: false, signupOpen: false }),
      }));
      const router = deps.getRouter();
      if (!router || router.state.location.pathname === SIGN_IN) return;
      void router.navigate(`${SIGN_IN}?returnTo=${encodeURIComponent(returnTo)}`, {
        state: { ended: true },
      });
    },
    onReadOnly: (readOnly) => setReadOnly(readOnly),
    onFailure: (error) => deps.reporter.apiFailure(error),
  });
}
