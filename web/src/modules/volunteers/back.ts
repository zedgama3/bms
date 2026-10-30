/** Where a shift page's back link goes (V4.2): router state a screen passes when it links to a
 *  shift, validated on read because history state survives reloads and deployments and can be
 *  absent on a deep link. Nothing personal travels in it: a module path and a fixed token. */
export type BackKind = "shifts" | "department" | "me";

export interface BackState {
  back: { to: string; kind: BackKind };
}

const KINDS: readonly string[] = ["shifts", "department", "me"];
const PATH = /^\/volunteers(\/[^?#]*)?(\?[^#]*)?$/;
const DEFAULT = { to: "/volunteers", kind: "shifts" } as const;

function dotSegment(seg: string): boolean {
  let d = seg;
  try {
    d = decodeURIComponent(seg);
  } catch {
    return true; // undecodable: not a path we made
  }
  return d === "." || d === ".." || d.includes("\\") || d.includes("/");
}

/** The state for a link from the current location. */
export function backTo(kind: BackKind, location: { pathname: string; search: string }): BackState {
  return { back: { to: `${location.pathname}${location.search}`, kind } };
}

/** The validated back link for a shift page: the state's, else `/volunteers` ("shifts"). */
export function backFrom(state: unknown): { to: string; kind: BackKind } {
  if (typeof state !== "object" || state === null) return { ...DEFAULT };
  const back = (state as { back?: unknown }).back;
  if (typeof back !== "object" || back === null) return { ...DEFAULT };
  const { to, kind } = back as { to?: unknown; kind?: unknown };
  if (typeof to !== "string" || typeof kind !== "string") return { ...DEFAULT };
  if (!KINDS.includes(kind) || !PATH.test(to) || to.includes("//") || to.includes("\\"))
    return { ...DEFAULT };
  if (to.split("?")[0]!.split("/").some(dotSegment)) return { ...DEFAULT };
  const path = to.split("?")[0]!;
  const fits =
    kind === "shifts"
      ? path === "/volunteers"
      : kind === "me"
        ? path === "/volunteers/me"
        : path.startsWith("/volunteers/departments/");
  if (!fits) return { ...DEFAULT };
  return { to, kind: kind as BackKind };
}

/** The back state a screen received, to pass on unchanged (the shift form keeps it through Edit
 *  and Save). Still validated by `backFrom` where it is read. */
export function carry(location: { state: unknown }): unknown {
  const state: unknown = location.state;
  return state;
}
