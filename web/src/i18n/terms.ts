import type { Catalogue } from "./catalogue";

const cache = new WeakMap<Catalogue, Record<string, string>>();

const MODULE_TERM = /^module\.[a-z][a-z0-9_-]*\.term\.(.+)$/;

/** `{term_event}`, `{term_department}`, ...: every `term.*` entry of the catalogue, with the
 *  dots of the rest of the key turned into underscores (decision 7). A module's declared
 *  terms come the same way from `module.<name>.term.<key>[.<form>]`, as the default; a
 *  `term.<key>` entry (the regional's word from the public document, or the shell's own
 *  core entries) takes precedence over it (module-volunteers.md Amendment V3, D57). */
export function termsOf(messages: Catalogue): Record<string, string> {
  const hit = cache.get(messages);
  if (hit) return hit;
  const out: Record<string, string> = {};
  const argOf = (rest: string) => "term_" + rest.replaceAll(".", "_");
  for (const [k, v] of Object.entries(messages)) {
    const m = MODULE_TERM.exec(k);
    if (m?.[1] !== undefined) out[argOf(m[1])] = v;
  }
  for (const [k, v] of Object.entries(messages)) {
    if (k.startsWith("term.")) out[argOf(k.slice(5))] = v;
  }
  cache.set(messages, out);
  return out;
}
