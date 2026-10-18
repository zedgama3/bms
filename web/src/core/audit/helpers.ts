import type { Session } from "@/api";

/** Whether the session shows a permission, site-wide or for some department (a convenience for
 *  hiding buttons; the server decides every call). */
export function holds(session: Session, permission: string): boolean {
  return (
    session.signedIn &&
    session.permissions.some((p) => p === permission || p.startsWith(`${permission}@`))
  );
}
