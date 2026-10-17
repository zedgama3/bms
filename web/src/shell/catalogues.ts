import shellEn from "./messages/en.json";
import { addCatalogue, hasCatalogue } from "@/i18n/internal";
import type { ScreenSet } from "@/registry/types";

/** The shell's own catalogues, by language tag; `en` is the reference. */
export const shellMessages: Readonly<
  Record<string, () => Promise<Readonly<Record<string, string>>>>
> = {
  // Static: the reference catalogue is in the entry chunk, so boot (and the cannot-reach-the-
  // server state) never depends on a second request that could itself fail.
  en: () => Promise.resolve(shellEn),
};

/** The languages the shell's catalogue has: what the language menu offers (decision 7). */
export const shellLanguages: readonly string[] = Object.keys(shellMessages);

async function load(
  owner: string,
  lang: string,
  loaders: Readonly<Record<string, () => Promise<Readonly<Record<string, string>>>>>,
): Promise<void> {
  if (hasCatalogue(owner, lang)) return;
  const loader = loaders[lang];
  if (!loader) return;
  addCatalogue(owner, lang, await loader());
}

/** Loads `en` and the active language for the shell and for each given set. A language a
 *  set lacks is skipped (its keys fall back to `en`, F23). */
export async function loadCatalogues(sets: readonly ScreenSet[], lang: string): Promise<void> {
  const langs = lang === "en" ? ["en"] : ["en", lang];
  const jobs: Promise<void>[] = [];
  for (const l of langs) {
    jobs.push(load("shell", l, shellMessages));
    for (const s of sets) jobs.push(load(s.name, l, s.messages));
  }
  await Promise.all(jobs);
}
