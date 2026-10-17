// The loaded message catalogues: one per (owner, language), where an owner is the shell or a
// ScreenSet. Merged per language for the IntlProvider.

export type Catalogue = Readonly<Record<string, string>>;

const loaded = new Map<string, Map<string, Catalogue>>(); // lang -> owner -> messages
const merged = new Map<string, Catalogue>();
const listeners = new Set<() => void>();
let version = 0;

export function addCatalogue(owner: string, lang: string, messages: Catalogue): void {
  let byOwner = loaded.get(lang);
  if (!byOwner) {
    byOwner = new Map();
    loaded.set(lang, byOwner);
  }
  byOwner.set(owner, messages);
  merged.delete(lang);
  version++;
  for (const l of listeners) l();
}

export function hasCatalogue(owner: string, lang: string): boolean {
  return loaded.get(lang)?.has(owner) ?? false;
}

/** Every loaded message of a language, merged. */
export function mergedFor(lang: string): Catalogue {
  const hit = merged.get(lang);
  if (hit) return hit;
  const out: Record<string, string> = {};
  for (const cat of loaded.get(lang)?.values() ?? []) Object.assign(out, cat);
  merged.set(lang, out);
  return out;
}

export function catalogueVersion(): number {
  return version;
}

export function subscribeCatalogues(l: () => void): () => void {
  listeners.add(l);
  return () => {
    listeners.delete(l);
  };
}

/** Tests only. */
export function resetCataloguesForTest(): void {
  loaded.clear();
  merged.clear();
  listeners.clear();
  version = 0;
}
