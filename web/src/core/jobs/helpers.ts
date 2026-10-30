export { holdsSite } from "@/api";

/** The server's cap on each list of the status (jobs.md `MaxRecent`, J-W1 decision 25). */
export const MAX_RECENT = 50;

/** `identity.password-reset` is owner `identity`, ident `password-reset`. */
export function splitName(name: string): { owner: string; ident: string } {
  const i = name.indexOf(".");
  return i < 0 ? { owner: "", ident: name } : { owner: name.slice(0, i), ident: name.slice(i + 1) };
}

/** A span of seconds as one whole unit for an ICU select: the largest that divides it evenly. */
export function spanOf(seconds: number): { unit: "day" | "hour" | "minute" | "second"; n: number } {
  const s = Math.max(0, Math.round(seconds));
  if (s >= 86400 && s % 86400 === 0) return { unit: "day", n: s / 86400 };
  if (s >= 3600 && s % 3600 === 0) return { unit: "hour", n: s / 3600 };
  if (s >= 60 && s % 60 === 0) return { unit: "minute", n: s / 60 };
  return { unit: "second", n: s };
}

/** Whole seconds between two instants, or null when either is missing or unreadable. */
export function secondsBetween(from: string | undefined, to: string | undefined): number | null {
  if (from === undefined || to === undefined) return null;
  const a = Date.parse(from);
  const b = Date.parse(to);
  if (Number.isNaN(a) || Number.isNaN(b) || b < a) return null;
  return Math.round((b - a) / 1000);
}

/** The selector for an element by id (names have dots). */
export function byId(id: string): string {
  return `[id="${CSS.escape(id)}"]`;
}
