import type { Session } from "./session";

/** The two queries the frame boots from. A 401 keeps both (sign-in needs them). */
export const MODULES_KEY = ["core", "modules"] as const;
export const SESSION_KEY = ["core", "session"] as const;

export interface SessionResult {
  /** False while the identity endpoints answer 404 (M0). */
  identity: boolean;
  session: Session;
}

export function isBootKey(key: readonly unknown[]): boolean {
  const k = JSON.stringify(key);
  return k === JSON.stringify(MODULES_KEY) || k === JSON.stringify(SESSION_KEY);
}
