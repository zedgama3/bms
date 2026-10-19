import { isMessageKey, useT } from "@/i18n";
import type { EventKind } from "./types";

/** The English names migration 0001 seeds for the builtin kinds. The API has no "renamed" flag,
 *  so a builtin whose stored name still equals its seeded name counts as unrenamed (D38). */
const SEEDED_NAMES: Readonly<Record<string, string>> = {
  burn: "Burn",
  decompression: "Decompression",
  other: "Other",
};

/** A kind's title: the catalogue label for an unrenamed builtin (so it translates), otherwise
 *  the stored name. */
export function useKindTitle(): (kind: EventKind) => string {
  const t = useT();
  return (kind) => {
    const catalogue = `core.events.kind.${kind.key}`;
    if (kind.builtin && kind.name === SEEDED_NAMES[kind.key] && isMessageKey(catalogue)) {
      return t(catalogue);
    }
    return kind.name;
  };
}

/** A kind's label by key: as `useKindTitle`, and the raw key for a kind no longer listed. */
export function useKindLabel(kinds: readonly EventKind[]): (key: string) => string {
  const title = useKindTitle();
  return (key) => {
    const kind = kinds.find((k) => k.key === key);
    return kind ? title(kind) : key;
  };
}
