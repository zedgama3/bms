import { useSyncExternalStore } from "react";

// The regional's name and default language: the shipped defaults until
// GET /api/regional/public answers (web-shell.md "Settings", regional.md).
export const DEFAULT_REGIONAL_NAME = "BMS";
export const DEFAULT_LANGUAGE = "en";

let name = DEFAULT_REGIONAL_NAME;
const listeners = new Set<() => void>();

/** Sets the regional's name from the public document (an empty name keeps the default). */
export function setRegionalName(next: string): void {
  const value = next.trim() === "" ? DEFAULT_REGIONAL_NAME : next;
  if (value === name) return;
  name = value;
  for (const l of listeners) l();
}

function subscribe(l: () => void): () => void {
  listeners.add(l);
  return () => {
    listeners.delete(l);
  };
}

export function useRegionalName(): string {
  return useSyncExternalStore(subscribe, () => name);
}

/** Tests only. */
export function resetRegionalForTest(): void {
  name = DEFAULT_REGIONAL_NAME;
  listeners.clear();
}
