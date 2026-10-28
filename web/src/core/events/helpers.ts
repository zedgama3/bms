export { holdsSite } from "@/api";

/** The browser's own zone, the default of a new event. */
export function browserZone(): string {
  return new Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
}

/** The IANA zones the browser knows, or [] when it cannot list them (the form then offers a
 *  text input). */
export function supportedZones(): string[] {
  try {
    const fn = (Intl as { supportedValuesOf?: (key: string) => string[] }).supportedValuesOf;
    return typeof fn === "function" ? fn("timeZone") : [];
  } catch {
    return [];
  }
}

function formatterFor(zone: string): Intl.DateTimeFormat | null {
  try {
    return new Intl.DateTimeFormat("en-US", {
      timeZone: zone,
      hourCycle: "h23",
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    });
  } catch {
    return null;
  }
}

/** The wall clock of an instant in a zone, as milliseconds read as if it were UTC. */
function wallClockMs(f: Intl.DateTimeFormat, ms: number): number {
  const p: Record<string, number> = {};
  for (const part of f.formatToParts(new Date(ms))) {
    if (part.type !== "literal") p[part.type] = Number(part.value);
  }
  return Date.UTC(
    p["year"] ?? 1970,
    (p["month"] ?? 1) - 1,
    p["day"] ?? 1,
    p["hour"] ?? 0,
    p["minute"] ?? 0,
    p["second"] ?? 0,
  );
}

const pad = (n: number) => String(n).padStart(2, "0");

/** An instant as the value of a `datetime-local` input, read in a zone ("" for a bad input). A
 *  zone the browser does not know reads as UTC; the server's `zone` error says what is wrong. */
export function instantToLocal(instant: string, zone: string): string {
  const ms = Date.parse(instant);
  if (Number.isNaN(ms)) return "";
  const f = formatterFor(zone) ?? formatterFor("UTC");
  if (!f) return "";
  const d = new Date(wallClockMs(f, ms));
  return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}T${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}`;
}

/** The value of a `datetime-local` input, read as wall-clock time in a zone, as an instant
 *  (RFC 3339, UTC); "" for an empty or malformed value. */
export function localToInstant(local: string, zone: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/.exec(local);
  if (!m) return "";
  const asUtc = Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3]), Number(m[4]), Number(m[5]));
  const f = formatterFor(zone);
  if (!f) return new Date(asUtc).toISOString();
  const offset = (ms: number) => wallClockMs(f, ms) - ms;
  const first = asUtc - offset(asUtc);
  const second = asUtc - offset(first);
  return new Date(second).toISOString();
}
