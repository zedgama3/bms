import type { Session } from "./session";

/** The queries the frame boots from. A 401 keeps all three (sign-in needs them). */
export const MODULES_KEY = ["core", "modules"] as const;
export const SESSION_KEY = ["core", "session"] as const;
/** The public settings document (name, languages, terms, theme): fetched at boot, refetched
 *  after the settings screens save. core/settings invalidates this same key. */
export const PUBLIC_KEY = ["core", "settings", "public"] as const;

/** GET /api/demo: whether this is a public demo (the banner), read once at boot. */
export const DEMO_KEY = ["core", "demo"] as const;

export interface SessionResult {
  /** False while the identity endpoints answer 404 (M0). */
  identity: boolean;
  session: Session;
}

export function isBootKey(key: readonly unknown[]): boolean {
  const k = JSON.stringify(key);
  return (
    k === JSON.stringify(MODULES_KEY) ||
    k === JSON.stringify(SESSION_KEY) ||
    k === JSON.stringify(PUBLIC_KEY) ||
    k === JSON.stringify(DEMO_KEY)
  );
}
