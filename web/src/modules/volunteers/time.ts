// Wall-clock times in an IANA zone <-> instants (a shift's times are shown and entered in the
// shift's own zone, not the browser's).

const parts = (ms: number, zone: string) => {
  const f = new Intl.DateTimeFormat("en-US", {
    timeZone: zone,
    hourCycle: "h23",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
  const out: Record<string, number> = {};
  for (const p of f.formatToParts(new Date(ms)))
    if (p.type !== "literal") out[p.type] = Number(p.value);
  return out as Record<"year" | "month" | "day" | "hour" | "minute" | "second", number>;
};

const pad = (n: number, w = 2) => String(n).padStart(w, "0");

function validZone(zone: string): string {
  try {
    new Intl.DateTimeFormat("en", { timeZone: zone });
    return zone;
  } catch {
    return "UTC";
  }
}

/** "YYYY-MM-DD" of an instant in a zone (groups shifts into days). */
export function dayKey(instant: string, zone: string): string {
  const ms = Date.parse(instant);
  if (Number.isNaN(ms)) return "";
  const p = parts(ms, validZone(zone));
  return `${pad(p.year, 4)}-${pad(p.month)}-${pad(p.day)}`;
}

/** The value of a `datetime-local` input ("YYYY-MM-DDTHH:mm") for an instant, in a zone. */
export function toLocalInput(instant: string, zone: string): string {
  const ms = Date.parse(instant);
  if (Number.isNaN(ms)) return "";
  const p = parts(ms, validZone(zone));
  return `${pad(p.year, 4)}-${pad(p.month)}-${pad(p.day)}T${pad(p.hour)}:${pad(p.minute)}`;
}

/** The instant (RFC 3339, UTC) of a `datetime-local` value read as wall-clock time in a zone;
 *  "" for a malformed value. */
export function fromLocalInput(local: string, zone: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/.exec(local);
  if (!m) return "";
  const asUtc = Date.UTC(+m[1]!, +m[2]! - 1, +m[3]!, +m[4]!, +m[5]!);
  const z = validZone(zone);
  const offsetAt = (ms: number) => {
    const p = parts(ms, z);
    return Date.UTC(p.year, p.month - 1, p.day, p.hour, p.minute, p.second) - ms;
  };
  let ms = asUtc - offsetAt(asUtc);
  ms = asUtc - offsetAt(ms); // once more across a daylight-saving change
  return new Date(ms).toISOString();
}

/** The zones the browser knows, for a standing shift's zone picker. */
export function zoneNames(): string[] {
  const fn = (Intl as unknown as { supportedValuesOf?: (k: string) => string[] }).supportedValuesOf;
  const list = fn ? fn("timeZone") : [];
  return list.length > 0 ? list : ["UTC"];
}

export function browserZone(): string {
  return new Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
}

/** The instants to ask the server for when listing one day's shifts: the day in `zone`, widened
 *  by a day each side because a shift's own zone may differ (the screen then keeps the shifts
 *  whose own day matches). */
export function dayWindow(day: string, zone: string): { from: string; to: string } | null {
  const start = Date.parse(fromLocalInput(`${day}T00:00`, zone));
  if (Number.isNaN(start)) return null;
  const DAY = 24 * 60 * 60 * 1000;
  return { from: new Date(start - DAY).toISOString(), to: new Date(start + 2 * DAY).toISOString() };
}
