// Defaults used until `settings` exists (web-shell.md "Settings"): the regional's name and
// default language. GET /api/settings/public will replace these (Proposed change 7).
export const DEFAULT_REGIONAL_NAME = "BMS";
export const DEFAULT_LANGUAGE = "en";

export function useRegionalName(): string {
  return DEFAULT_REGIONAL_NAME;
}
