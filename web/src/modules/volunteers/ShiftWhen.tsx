import { useFormat, useT } from "@/i18n";
import type { Shift } from "./types";

/** A shift's times in its own zone ("Oct 21, 2026, 9:00 AM to 1:00 PM"); the zone is named when
 *  it differs from the browser's. */
export function ShiftWhen({ shift }: { shift: Pick<Shift, "starts" | "ends" | "zone"> }) {
  const t = useT();
  const format = useFormat();
  return (
    <time dateTime={shift.starts}>
      {t("module.volunteers.when", {
        start: format.dateTime(shift.starts, shift.zone),
        end: format.time(shift.ends, shift.zone),
      })}
    </time>
  );
}
