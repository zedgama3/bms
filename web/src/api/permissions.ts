import type { Session } from "./session";

/** Whether the session shows a site-wide permission (a convenience for hiding buttons; the
 *  server decides every call). */
export function holdsSite(session: Session, permission: string): boolean {
  return session.signedIn && session.permissions.includes(permission);
}

/** Whether the session shows a permission site-wide or at one department. */
export function holdsAt(session: Session, permission: string, department: string): boolean {
  return (
    session.signedIn &&
    (session.permissions.includes(permission) ||
      session.permissions.includes(`${permission}@${department}`))
  );
}

/** Whether the session shows a permission at all (site-wide or at any department). */
export function holdsAny(session: Session, permission: string): boolean {
  return (
    session.signedIn &&
    session.permissions.some((p) => p === permission || p.startsWith(`${permission}@`))
  );
}
