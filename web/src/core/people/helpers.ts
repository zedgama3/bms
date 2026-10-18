import type { Session } from "@/api";

/** Whether the session shows a permission, site-wide or for some department (a convenience for
 *  hiding buttons; the server decides every call). */
export function holds(session: Session, permission: string): boolean {
  return (
    session.signedIn &&
    session.permissions.some((p) => p === permission || p.startsWith(`${permission}@`))
  );
}

/** A route the server put in data (a section item's href): a same-origin path or nothing. */
export function safeHref(href: string): string {
  if (!href.startsWith("/") || href.startsWith("//") || href.startsWith("/\\")) return "";
  // eslint-disable-next-line no-control-regex
  return /[\u0000-\u001f\u007f]/.test(href) ? "" : href;
}
