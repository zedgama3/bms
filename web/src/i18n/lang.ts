// The language choice: the regional's default, then the person's explicit choice kept in
// localStorage["bms.lang"] (decision 7). This is the only file that touches localStorage.

const STORAGE_KEY = "bms.lang";

let offered: readonly string[] = ["en"];
let fallback = "en";
let current = "en";
const listeners = new Set<() => void>();

function emit(): void {
  document.documentElement.lang = current;
  for (const l of listeners) l();
}

function readStored(): string | null {
  try {
    return localStorage.getItem(STORAGE_KEY);
  } catch {
    return null; // private mode, quota, blocked: the choice lives in memory (F25)
  }
}

/** Sets the languages on offer and the regional's default, then re-reads the stored choice
 *  (a stored tag that is not offered is ignored, F26). */
export function configureLanguages(langs: readonly string[], defaultLang = "en"): void {
  offered = langs.includes("en") ? [...langs] : ["en", ...langs];
  fallback = offered.includes(defaultLang) ? defaultLang : "en";
  const stored = readStored();
  current = stored !== null && offered.includes(stored) ? stored : fallback;
  emit();
}

export function getLanguage(): string {
  return current;
}

export function getOffered(): readonly string[] {
  return offered;
}

export function subscribeLanguage(l: () => void): () => void {
  listeners.add(l);
  return () => {
    listeners.delete(l);
  };
}

/** Persists the choice (when storage works) and re-renders. A tag that is not offered is ignored. */
export function setLanguage(tag: string): void {
  if (!offered.includes(tag) || tag === current) return;
  current = tag;
  try {
    localStorage.setItem(STORAGE_KEY, tag);
  } catch {
    // ignored: the choice lives in memory for the page (F25)
  }
  emit();
}

/** Tests only. */
export function resetLanguageForTest(): void {
  offered = ["en"];
  fallback = "en";
  current = "en";
  listeners.clear();
}
