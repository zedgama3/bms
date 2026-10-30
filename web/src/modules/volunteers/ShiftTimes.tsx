import { useFormat, useT } from "@/i18n";
import { dayKey } from "./time";
import type { Shift } from "./types";

/** A shift's times only ("9:00 AM to 1:00 PM"), for a row under its day heading; a night shift
 *  says "next day" so "8:00 PM to 2:00 AM" is not read backwards. */
export function ShiftTimes({ shift }: { shift: Pick<Shift, "starts" | "ends" | "zone"> }) {
  const t = useT();
  const format = useFormat();
  const days = Math.round(
    (Date.parse(`${dayKey(shift.ends, shift.zone)}T00:00:00Z`) -
      Date.parse(`${dayKey(shift.starts, shift.zone)}T00:00:00Z`)) /
      86_400_000,
  );
  const nextDay = days === 1;
  return (
    <time dateTime={shift.starts}>
      {t(nextDay ? "module.volunteers.when_next_day" : "module.volunteers.when", {
        start: format.time(shift.starts, shift.zone),
        // Two or more days later "next day" would be wrong: give the end's date.
        end:
          days >= 2 ? format.dateTime(shift.ends, shift.zone) : format.time(shift.ends, shift.zone),
      })}
    </time>
  );
}
