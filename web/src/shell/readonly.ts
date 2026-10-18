import { getReadOnly, setReadOnlyState } from "@/api/internal";
import { useReadOnly } from "@/api";
import type { ReadOnlyState } from "@/api";

export type { ReadOnlyState };
export { getReadOnly, useReadOnly };

/** From X-Read-Only on every response: the banner follows the header both ways (F12). */
export function setReadOnly(readOnly: boolean, now: () => Date = () => new Date()): void {
  const cur = getReadOnly();
  if (cur.readOnly === readOnly) return;
  setReadOnlyState(readOnly ? { readOnly, since: now() } : { readOnly });
}
