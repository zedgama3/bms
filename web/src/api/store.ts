import { useSyncExternalStore } from "react";

/** A tiny memory-only store read with useSyncExternalStore (session and read-only state). */
export interface Store<T> {
  get: () => T;
  set: (next: T) => void;
  subscribe: (l: () => void) => () => void;
  use: () => T;
}

export function createStore<T>(initial: T): Store<T> {
  let value = initial;
  const listeners = new Set<() => void>();
  const store: Store<T> = {
    get: () => value,
    set(next) {
      if (Object.is(next, value)) return;
      value = next;
      for (const l of listeners) l();
    },
    subscribe(l) {
      listeners.add(l);
      return () => {
        listeners.delete(l);
      };
    },
    use: () => useSyncExternalStore(store.subscribe, store.get),
  };
  return store;
}
