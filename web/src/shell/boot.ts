import { createContext, useContext } from "react";

/** What the boot step learned that the frame shows: whether the session check failed (F29)
 *  and whether the identity endpoints exist yet (F28). */
export interface BootState {
  /** The session call failed other than with 404: the frame shows the banner. */
  sessionFailed: boolean;
  retrySession: () => void;
  /** False while GET /api/accounts/session answers 404 (M0, no identity module). */
  identityAvailable: boolean;
  /** Re-reads the session after sign-in or sign-out. */
  refreshSession: () => Promise<void>;
  /** A catalogue chunk failed to load: the frame offers a retry. */
  catalogueFailed: boolean;
  retryCatalogues: () => void;
  /** GET /api/demo/ failed (other than 404): the banner is hidden, so the frame offers a retry. */
  demoFailed: boolean;
  retryDemo: () => void;
  /** Leaves the single-page app for a fresh load (`location.replace`), so nothing keeps old data. */
  leave: (to: string) => void;
}

export const BootContext = createContext<BootState>({
  sessionFailed: false,
  retrySession() {},
  identityAvailable: false,
  async refreshSession() {},
  catalogueFailed: false,
  retryCatalogues() {},
  demoFailed: false,
  retryDemo() {},
  leave: (to) => window.location.replace(to),
});

export function useBoot(): BootState {
  return useContext(BootContext);
}

export { safeReturnTo } from "@/api";

/** `/people/42` with params {id: "42"} becomes `/people/{id}` (the reporter's screen pattern). */
export function patternOf(
  pathname: string,
  params: Readonly<Record<string, string | undefined>>,
): string {
  const entries = Object.entries(params).filter(
    (e): e is [string, string] => e[1] !== undefined && e[0] !== "*",
  );
  const splat = params["*"];
  let path = pathname;
  if (splat) {
    const i = path.lastIndexOf(splat);
    if (i > 0) path = path.slice(0, i) + "{rest}";
  }
  // Params appear in the path in order: take them in turn, so two params with the same value
  // (`/notify/templates/a/a`) still get their own names.
  let next = 0;
  return (
    path
      .split("/")
      .map((seg) => {
        if (seg === "{rest}") return seg;
        const hit = entries[next];
        if (hit && hit[0] !== "*" && hit[1] === seg) {
          next++;
          return `{${hit[0]}}`;
        }
        return seg;
      })
      .join("/") || "/"
  );
}
