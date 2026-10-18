import type { Session } from "@/api";

/** Whether the session shows a permission site-wide (a convenience for hiding buttons; the
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

/** The first line of a description, for a card. */
export function firstLine(description: string): string {
  return description.split("\n", 1)[0]?.trim() ?? "";
}
