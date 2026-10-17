import type { ApiError } from "@/api";
import { isMessageKey } from "@/i18n";
import type { Translate } from "@/i18n";

/** True for every code except `invalid`, `unauthenticated`, `read_only` and the client code
 *  `network` (decision 9), and only when a reference exists. */
export function showsRef(error: ApiError): boolean {
  if (error.ref === undefined) return false;
  return !["invalid", "unauthenticated", "read_only", "network"].includes(error.code);
}

/** True when the catalogue has no sentence for the error's code (a server newer than the shell). */
export function isUntranslatedCode(error: ApiError): boolean {
  return !isMessageKey(`error.${error.code}`);
}

/** The translated sentence for an error, by code: `error.<code>`, or `error.unknown` for a
 *  code the catalogue lacks (a problem body carries no `reason` at the top level, so there
 *  is nothing else to show; the code is reported, decision 2). */
export function messageFor(t: Translate, error: ApiError): string {
  const key = `error.${error.code}`;
  if (!isMessageKey(key)) return t("error.unknown");
  // No Retry-After, no invented number.
  if (error.code === "rate_limited" && error.retryAfter === undefined)
    return t("error.rate_limited.generic");
  return t(key, { seconds: error.retryAfter ?? 0 });
}
