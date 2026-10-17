import { createStore } from "@/api/store";

export type ReadOnlyState = { readOnly: boolean; since?: Date };

const store = createStore<ReadOnlyState>({ readOnly: false });

export const getReadOnly = store.get;
export const useReadOnly = store.use;

/** From X-Read-Only on every response: the banner follows the header both ways (F12). */
export function setReadOnly(readOnly: boolean, now: () => Date = () => new Date()): void {
  const cur = store.get();
  if (cur.readOnly === readOnly) return;
  store.set(readOnly ? { readOnly, since: now() } : { readOnly });
}
