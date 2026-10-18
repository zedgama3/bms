import { createStore } from "./store";
import type { ID } from "./errors";

// Held in memory by the API layer so screens can read who is signed in (screens read it through
// useSession()). Filled from GET /api/accounts/session (identity.md "SessionInfo"; permissions
// from authz.md, Proposed change 6). `signupOpen` is also sent when signed out.
export type Session =
  | { signedIn: false; signupOpen: boolean }
  | {
      signedIn: true;
      personId: ID;
      accountId: ID;
      /** "" until the person has a profile (M2): the frame shows a neutral label. */
      playaName: string;
      superuser: boolean;
      /** A temporary password: only the password screen works until it is changed. */
      passwordChangeRequired: boolean;
      expiresAt: Date | null;
      signupOpen: boolean;
      permissions: readonly string[];
    };

export const SIGNED_OUT: Session = { signedIn: false, signupOpen: false };

const store = createStore<Session>(SIGNED_OUT);

export const getSession = store.get;
export const setSession = store.set;
export const subscribeSession = store.subscribe;
/** The current session (private to the frame; screens ask the server). */
export const useSession = store.use;
/** Signed out, remembering whether sign-up is open (the sign-in screen offers it). */
export function signedOutLike(prev: Session): Session {
  return prev.signupOpen ? { signedIn: false, signupOpen: true } : SIGNED_OUT;
}
export function resetSession(): void {
  store.set(signedOutLike(store.get()));
}

/** The wire form of GET /api/accounts/session: unknown shapes mean signed out. */
export function parseSession(body: unknown): Session {
  if (typeof body !== "object" || body === null) return SIGNED_OUT;
  const b = body as Record<string, unknown>;
  const signupOpen = b["signup_open"] === true;
  if (b["signed_in"] !== true) return signupOpen ? { signedIn: false, signupOpen } : SIGNED_OUT;
  const perms = Array.isArray(b["permissions"])
    ? (b["permissions"] as unknown[]).filter((p): p is string => typeof p === "string")
    : [];
  const exp = typeof b["expires_at"] === "string" ? new Date(b["expires_at"]) : null;
  return {
    signedIn: true,
    personId: typeof b["person_id"] === "string" ? b["person_id"] : "",
    accountId: typeof b["account_id"] === "string" ? b["account_id"] : "",
    playaName: typeof b["playa_name"] === "string" ? b["playa_name"] : "",
    superuser: b["superuser"] === true,
    passwordChangeRequired: b["password_change_required"] === true,
    expiresAt: exp !== null && !Number.isNaN(exp.getTime()) ? exp : null,
    signupOpen,
    permissions: perms,
  };
}

let refresher: () => Promise<void> = async () => {};
/** The frame installs how to re-read the session (it owns the query client). */
export function registerSessionRefresh(fn: () => Promise<void>): void {
  refresher = fn;
}
/** Re-reads the session after sign-in, sign-up, sign-out or a password change; resolves once
 *  the session store holds the new answer, so a following navigation sees it. */
export function refreshSession(): Promise<void> {
  return refresher();
}

/** Set when a 401 ended the session, so the sign-in page can say so once (a toast). */
const ended = createStore(false);
export const getSessionEnded = ended.get;
export const setSessionEnded = ended.set;

/** Whether the boot session check failed (not "signed out": the answer is unknown), so a
 *  screen that depends on `signupOpen` can say it could not tell. */
const failed = createStore(false);
export const setSessionCheckFailed = failed.set;
export const useSessionCheckFailed = failed.use;

/** `returnTo` must be a same-origin path: begins with `/`, not `//` or `/\` (F34). */
export function safeReturnTo(raw: string | null): string {
  if (!raw || !raw.startsWith("/") || raw.startsWith("//") || raw.startsWith("/\\")) return "/";
  // eslint-disable-next-line no-control-regex
  if (/[\u0000-\u001f\u007f]/.test(raw)) return "/";
  return raw;
}
