import type { components } from "@/api";
import { DEFAULT_PUBLIC, publicStore } from "@/api/internal";
import { addCatalogue, configureLanguages, REGIONAL_OWNER } from "@/i18n/internal";
import { shellLanguages } from "./catalogues";
import { DEFAULT_LANGUAGE, setRegionalName } from "./regional";

export type PublicDoc = components["schemas"]["RegionalPublic"];

const THEME_ID = "bms-theme";
const THEME_HREF = "/api/regional/theme.css";

/** `term_theme_camp_plural_title` becomes the catalogue key `term.theme_camp.plural_title`
 *  (the inverse of `termsOf`); the singular is `term.theme_camp`. */
export function termKey(arg: string): string | null {
  if (!arg.startsWith("term_")) return null;
  const rest = arg.slice(5);
  for (const form of ["plural_title", "plural", "title"]) {
    if (rest.endsWith(`_${form}`) && rest.length > form.length + 1) {
      return `term.${rest.slice(0, -(form.length + 1))}.${form}`;
    }
  }
  return `term.${rest}`;
}

function catalogueOf(args: Readonly<Record<string, string>>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(args)) {
    const key = termKey(k);
    if (key !== null && v !== "") out[key] = v;
  }
  return out;
}

let appliedTerms = "";
let appliedLanguages = "";
let themeVersion = 0;

/** Links, re-links or removes the theme. An existing link is left alone unless `reload` (a save
 *  changed the colours, which the public document does not show). */
function applyTheme(on: boolean, reload: boolean): void {
  const existing = document.getElementById(THEME_ID);
  if (!on) {
    existing?.remove();
    return;
  }
  if (existing && !reload) return;
  // A new query string each time, so a saved colour is fetched again (the API says no-store).
  themeVersion++;
  const href = `${THEME_HREF}?v=${themeVersion}`;
  if (existing instanceof HTMLLinkElement) {
    existing.href = href;
    return;
  }
  const link = document.createElement("link");
  link.id = THEME_ID;
  link.rel = "stylesheet";
  link.href = href;
  // Last in the head, so it follows the built stylesheet and overrides its colour tokens.
  document.head.append(link);
}

/** Applies the public document to the frame: the regional's name, the languages on offer and
 *  the default, the terms (merged over the catalogue's `term.*` entries) and the theme link. */
export function applyPublic(doc: PublicDoc, reloadTheme = false): void {
  setRegionalName(doc.name);
  // The name and the logo's file ID, for the header and the settings screen (`usePublic`).
  const name = doc.name.trim() === "" ? DEFAULT_PUBLIC.name : doc.name;
  // A server older than the logo sends no field: no logo.
  const logo = doc.logo || "";
  const now = publicStore.get();
  if (now.name !== name || now.logo !== logo) publicStore.set({ name, logo });

  const offered = shellLanguages.filter((l) => doc.languages.includes(l));
  const languages = `${offered.join(",")}|${doc.language}`;
  if (languages !== appliedLanguages) {
    appliedLanguages = languages;
    configureLanguages(
      offered.length > 0 ? offered : shellLanguages,
      doc.language || DEFAULT_LANGUAGE,
    );
  }

  const terms = JSON.stringify(doc.terms);
  if (terms !== appliedTerms) {
    appliedTerms = terms;
    for (const [lang, args] of Object.entries(doc.terms)) {
      addCatalogue(REGIONAL_OWNER, lang, catalogueOf(args));
    }
  }

  applyTheme(doc.theme, reloadTheme);
}

/** Tests only. */
export function resetPublicForTest(): void {
  appliedTerms = "";
  appliedLanguages = "";
  themeVersion = 0;
  publicStore.set(DEFAULT_PUBLIC);
  document.getElementById(THEME_ID)?.remove();
}
