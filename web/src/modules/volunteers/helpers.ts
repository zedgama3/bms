import type { Session } from "@/api";
export { holdsSite, holdsAt, holdsAny } from "@/api";

/** The departments at which the session shows any of the permissions: `site` when one is
 *  held site-wide, else the IDs from the `p@D` entries. */
export function ledDepartments(
  session: Session,
  permissions: readonly string[],
): { site: boolean; ids: string[] } {
  if (!session.signedIn) return { site: false, ids: [] };
  const ids = new Set<string>();
  let site = false;
  for (const held of session.permissions) {
    const at = held.indexOf("@");
    const name = at < 0 ? held : held.slice(0, at);
    if (!permissions.includes(name)) continue;
    if (at < 0) site = true;
    else ids.add(held.slice(at + 1));
  }
  return { site, ids: [...ids] };
}

/** A form value as a string (a file or a missing field is ""). */
export const text = (v: FormDataEntryValue | null): string => (typeof v === "string" ? v : "");

/** A civil date (YYYY-MM-DD) as a Date at local noon, so a locale format never shifts the day. */
export function civilDate(s: string): Date {
  const [y, m, d] = s.split("-").map(Number);
  return new Date(y ?? 1970, (m ?? 1) - 1, d ?? 1, 12);
}

/** A civil date (YYYY-MM-DD) plus whole days, as a civil date; "" for a malformed date. */
export function addDays(date: string, days: number): string {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) return "";
  const d = civilDate(date);
  d.setDate(d.getDate() + days);
  const pad = (n: number, w = 2) => String(n).padStart(w, "0");
  return `${pad(d.getFullYear(), 4)}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/** The entries of an object whose value is not "" (an empty required field is left out of a
 *  request so the service answers `required` on that field, not `malformed` on the body). */
export function omitEmpty<T extends Record<string, string | number | undefined>>(
  obj: T,
): Partial<T> {
  return Object.fromEntries(
    Object.entries(obj).filter(([, v]) => v !== "" && v !== undefined),
  ) as Partial<T>;
}

/** Moves focus to a live element after the one that opened a dialog is gone. A closing modal
 *  dialog gives focus back to its opener (or drops it), so this retries for a moment until the
 *  element holds it. */
export function focusSoon(selector: string): void {
  let tries = 0;
  const attempt = () => {
    const el = document.querySelector<HTMLElement>(selector);
    if (el) {
      if (el.tabIndex < 0 && !el.hasAttribute("tabindex")) el.setAttribute("tabindex", "-1");
      el.focus();
      if (document.activeElement === el) return;
    }
    tries += 1;
    if (tries < 10) setTimeout(attempt, 30);
  };
  setTimeout(attempt, 0);
}

export type RecordStatus = "pending" | "verified" | "expired";

/** A record's status: pending until a lead verifies it; expired once its end date has passed
 *  (`today` is a civil date, YYYY-MM-DD). */
export function recordStatus(
  r: {
    verified_at?: string | undefined;
    verified_by?: string | undefined;
    expires_on?: string | undefined;
  },
  today: string,
): RecordStatus {
  const verified = Boolean(r.verified_at) || Boolean(r.verified_by);
  if (!verified) return "pending";
  if (r.expires_on && r.expires_on < today) return "expired";
  return "verified";
}
