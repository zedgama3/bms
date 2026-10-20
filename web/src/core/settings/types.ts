import type { components } from "@/api";

export type SettingsDocument = components["schemas"]["SettingsDocument"];
export type AreaDoc = components["schemas"]["SettingsArea"];
export type SettingDoc = components["schemas"]["SettingsSetting"];
export type TermsDocument = components["schemas"]["SettingsTermsDocument"];
export type TermChanges = components["schemas"]["SettingsTermChanges"];
export type FormChanges = components["schemas"]["SettingsFormChanges"];

/** The four forms of a term, in the API's names. */
export const FORMS = ["singular", "plural", "singular_title", "plural_title"] as const;
export type Form = (typeof FORMS)[number];

/** What a person has typed, by full setting key. Absent: untouched. A string: the new text
 *  (for a secret, the replacement). null: back to the default (for a secret, cleared). */
export type Drafts = Readonly<Record<string, string | null>>;
