import type { Session } from "@/api";

export {
  SIGNED_OUT,
  getSession,
  getSessionEnded,
  parseSession,
  resetSession,
  setSession,
  signedOutLike,
  subscribeSession,
} from "@/api/internal";
export { setSessionEnded, useSession } from "@/api";
export type { Session } from "@/api";

/** Whether a nav entry or route is shown before the server decides (a convenience, never access control). */
export function canSee(
  session: Session,
  access: "public" | "signed-in" | readonly string[] | undefined,
): boolean {
  if (access === "public") return true;
  if (!session.signedIn) return false;
  if (access === undefined || access === "signed-in") return true;
  // Before authz fills `permissions` (M2) a permission requirement shows the entry to every
  // signed-in person and the screen shows its forbidden state.
  if (session.permissions.length === 0) return true;
  return access.some((p) => session.permissions.includes(p));
}
