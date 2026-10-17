import { createStore } from "./store";
import type { ID } from "./errors";

// Held in memory by the API layer so screens can read who is signed in (screens read it through
// useSession()). Filled from GET /api/accounts/session (identity.md, Proposed change 5; permissions from
// authz.md, Proposed change 6). In M0 the route does not exist and the shell is signed out (F28).
export type Session =
  | { signedIn: false }
  | {
      signedIn: true;
      personId: ID;
      playaName: string;
      superuser: boolean;
      permissions: readonly string[];
    };

export const SIGNED_OUT: Session = { signedIn: false };

const store = createStore<Session>(SIGNED_OUT);

export const getSession = store.get;
export const setSession = store.set;
export const subscribeSession = store.subscribe;
/** The current session (private to the frame; screens ask the server). */
export const useSession = store.use;
export function resetSession(): void {
  store.set(SIGNED_OUT);
}

/** The wire form of the session response, which identity.md will define: unknown shapes mean signed out. */
export function parseSession(body: unknown): Session {
  if (typeof body !== "object" || body === null) return SIGNED_OUT;
  const b = body as Record<string, unknown>;
  if (b["signed_in"] !== true) return SIGNED_OUT;
  const perms = Array.isArray(b["permissions"])
    ? (b["permissions"] as unknown[]).filter((p): p is string => typeof p === "string")
    : [];
  return {
    signedIn: true,
    personId: typeof b["person_id"] === "string" ? b["person_id"] : "",
    playaName: typeof b["playa_name"] === "string" ? b["playa_name"] : "",
    superuser: b["superuser"] === true,
    permissions: perms,
  };
}
