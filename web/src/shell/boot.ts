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
  leave: (to) => window.location.replace(to),
});

export function useBoot(): BootState {
  return useContext(BootContext);
}

/** `returnTo` must be a same-origin path: begins with `/`, not `//` or `/\` (F34). */
export function safeReturnTo(raw: string | null): string {
  if (!raw || !raw.startsWith("/") || raw.startsWith("//") || raw.startsWith("/\\")) return "/";
  // eslint-disable-next-line no-control-regex
  if (/[\u0000-\u001f\u007f]/.test(raw)) return "/";
  return raw;
}

/** `/people/42` with params {id: "42"} becomes `/people/{id}` (the reporter's screen pattern). */
export function patternOf(
  pathname: string,
  params: Readonly<Record<string, string | undefined>>,
): string {
  const entries = Object.entries(params).filter((e): e is [string, string] => e[1] !== undefined);
  const splat = params["*"];
  let path = pathname;
  if (splat) {
    const i = path.lastIndexOf(splat);
    if (i > 0) path = path.slice(0, i) + "{rest}";
  }
  return (
    path
      .split("/")
      .map((seg) => {
        if (seg === "{rest}") return seg;
        const hit = entries.find(([k, v]) => k !== "*" && v === seg);
        return hit ? `{${hit[0]}}` : seg;
      })
      .join("/") || "/"
  );
}
