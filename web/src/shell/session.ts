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
  // The session holds `p` (site-wide) and `p@<department-id>`: an entry matches when the
  // session holds `p` exactly or any `p@...`. An empty list means none (M2).
  return access.some((p) =>
    session.permissions.some((held) => held === p || held.startsWith(`${p}@`)),
  );
}
