import type { Session } from "@/api";

/** Whether the session shows a permission site-wide (a convenience for hiding what the person
 *  can't do; the server decides every call). */
export function holdsSite(session: Session, permission: string): boolean {
  return session.signedIn && session.permissions.includes(permission);
}

/** `identity.password-reset` is owner `identity`, ident `password-reset`. */
export function splitName(name: string): { owner: string; ident: string } {
  const i = name.indexOf(".");
  return i < 0 ? { owner: "", ident: name } : { owner: name.slice(0, i), ident: name.slice(i + 1) };
}

/** The path of one template's edit screen. */
export function editPath(name: string, lang: string): string {
  return `/notify/templates/${encodeURIComponent(name)}/${encodeURIComponent(lang)}`;
}

/** The fields every template may use, besides its own `Vars` (notify.md "Template file grammar"). */
export const STANDARD_FIELDS: readonly string[] = [
  ".Site.Name",
  ".Site.URL",
  ".Recipient.PlayaName",
  ".Link",
];

/** True when the instant is not in the future. */
export function passed(instant: string | undefined): boolean {
  if (instant === undefined) return false;
  const ms = Date.parse(instant);
  return !Number.isNaN(ms) && ms <= Date.now();
}

/** A message's template as `<owner>.<ident>`, whether the record carries the whole name or only the ident. */
export function fullName(owner: string, template: string): string {
  return template.startsWith(`${owner}.`) ? template : `${owner}.${template}`;
}
