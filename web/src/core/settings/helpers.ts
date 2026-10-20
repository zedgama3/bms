import type { Session } from "@/api";
import type { SettingDoc } from "./types";

/** Whether the session shows a permission site-wide (a convenience for hiding what the person
 *  can't do; the server decides every call). */
export function holdsSite(session: Session, permission: string): boolean {
  return session.signedIn && session.permissions.includes(permission);
}

/** `accounts.session_lifetime` becomes `session_lifetime`. */
export function bareKey(full: string): string {
  const i = full.indexOf(".");
  return i < 0 ? full : full.slice(i + 1);
}

/** Whether a stored value exists that a reset would remove (a stale one counts). */
export function hasStored(s: SettingDoc): boolean {
  if (s.kind === "secret") return s.secret_set;
  return s.value !== null || s.stale;
}

/** The text a control shows before any edit: the default when the saved value is stale. */
export function baseText(s: SettingDoc): string {
  if (s.kind === "secret") return "";
  return s.stale ? s.default : (s.value ?? s.default);
}

/** The text a control shows for a draft. */
export function shownText(s: SettingDoc, draft: string | null | undefined): string {
  if (draft === undefined) return baseText(s);
  if (draft === null) return s.kind === "secret" ? "" : s.default;
  return draft;
}

/** The change to send for one setting, or undefined when nothing changed: a string sets, null
 *  resets to the default (or clears a secret). */
export function changeFor(
  s: SettingDoc,
  draft: string | null | undefined,
): string | null | undefined {
  if (draft === undefined) return undefined;
  if (s.kind === "secret") {
    if (draft === null) return s.secret_set ? null : undefined;
    return draft === "" ? undefined : draft;
  }
  if (draft === null) return hasStored(s) ? null : undefined;
  const text = draft.trim();
  if (text === s.default) return hasStored(s) ? null : undefined;
  if (!s.stale && s.value !== null && text === s.value) return undefined;
  return text;
}

/** Every change of a form, keyed by full setting key. */
export function changesOf(
  settings: readonly SettingDoc[],
  drafts: Readonly<Record<string, string | null>>,
): Record<string, string | null> {
  const out: Record<string, string | null> = {};
  for (const s of settings) {
    const c = changeFor(s, drafts[s.key]);
    if (c !== undefined) out[s.key] = c;
  }
  return out;
}

export type DurationUnit = "d" | "h" | "m" | "s";

/** A duration text ("720h", "15m", "1h30m") as a number and the largest unit that holds it
 *  exactly. Text that is not a duration yields an empty number. */
export function parseDuration(text: string): { n: string; unit: DurationUnit } {
  if (text.trim() === "0") return { n: "0", unit: "h" };
  const m = /^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s)?$/.exec(text.trim());
  if (!m || text.trim() === "") return { n: "", unit: "h" };
  const seconds = Number(m[1] ?? 0) * 3600 + Number(m[2] ?? 0) * 60 + Number(m[3] ?? 0);
  if (seconds > 0 && seconds % 86400 === 0) return { n: String(seconds / 86400), unit: "d" };
  if (seconds % 3600 === 0) return { n: String(seconds / 3600), unit: "h" };
  if (seconds % 60 === 0) return { n: String(seconds / 60), unit: "m" };
  return { n: String(seconds), unit: "s" };
}

/** A number and a unit as the API's duration text; days are written as hours. */
export function composeDuration(n: string, unit: DurationUnit): string {
  if (n === "") return "";
  if (unit === "d") return `${Number(n) * 24}h`;
  return `${n}${unit}`;
}

/** `#rrggbb` (case-insensitive) or null. */
export function parseHex(text: string): [number, number, number] | null {
  const m = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(text.trim());
  if (!m) return null;
  return [parseInt(m[1]!, 16), parseInt(m[2]!, 16), parseInt(m[3]!, 16)];
}

function luminance([r, g, b]: [number, number, number]): number {
  const lin = (c: number) => {
    const s = c / 255;
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

/** The WCAG contrast ratio between two colours. */
export function contrastRatio(a: [number, number, number], b: [number, number, number]): number {
  const la = luminance(a);
  const lb = luminance(b);
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}

/** The shipped backgrounds and the least contrast a brand colour needs (settings.md). */
export const LIGHT_BACKGROUND: [number, number, number] = [255, 255, 255];
export const DARK_BACKGROUND: [number, number, number] = [0x12, 0x12, 0x12];
export const MIN_CONTRAST = 4.5;
