import type { Catalogue } from "./catalogue";

const cache = new WeakMap<Catalogue, Record<string, string>>();

/** `{term_event}`, `{term_department}`, ...: every `term.*` entry of the catalogue, with the
 *  dots of the rest of the key turned into underscores (decision 7). Until `settings` supplies
 *  them per language, the catalogue's own entries are the regional's terms. */
export function termsOf(messages: Catalogue): Record<string, string> {
  const hit = cache.get(messages);
  if (hit) return hit;
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(messages)) {
    if (k.startsWith("term.")) out["term_" + k.slice(5).replaceAll(".", "_")] = v;
  }
  cache.set(messages, out);
  return out;
}
