import { isMessageKey, useT } from "@/i18n";
import type { EventKind } from "./types";

/** A kind's label: the catalogue key for a seeded kind (so it translates), the stored name for
 *  a kind the regional made, and the raw key for a kind no longer listed. */
export function useKindLabel(kinds: readonly EventKind[]): (key: string) => string {
  const t = useT();
  return (key) => {
    const kind = kinds.find((k) => k.key === key);
    const catalogue = `core.events.kind.${key}`;
    if (kind?.builtin && isMessageKey(catalogue)) return t(catalogue);
    return kind?.name ?? key;
  };
}
