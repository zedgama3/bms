// What a module or core area exports from its index.ts (web-shell.md "Interface").
import type { RouteObject } from "react-router";
import type { MessageKey } from "@/i18n";

export type Messages = Readonly<Record<string, string>>;

/** One module's or core area's screens. Exported as `ui` from web/src/modules/<name>/index.ts
 *  (or web/src/core/<area>/index.ts) and listed in the matching index.ts (D31). */
export interface ScreenSet {
  /** The module's name exactly as in its Go manifest ([a-z][a-z0-9]{1,31}), or the core
   *  area's name. Routes mount under `/<name>/`; a shell-reserved name is refused (F19). */
  readonly name: string;
  /** Core areas are always enabled; modules only when GET /api/modules lists them. */
  readonly kind: "module" | "core";
  /** Routes relative to `/<name>`. `path` is relative (no leading `/`, F20). Elements are
   *  lazy (`React.lazy`) so a module's code loads when first visited. A route's `handle`
   *  carries `{ title: MessageKey; access?: "public" | "signed-in" }` (default signed-in). */
  readonly routes: readonly RouteObject[];
  /** Entries for the frame's navigation. */
  readonly nav: readonly NavEntry[];
  /** Message catalogues by language tag, loaded on demand. `en` is required (F21). */
  readonly messages: Readonly<Record<string, () => Promise<Messages>>>;
  /** The usage actions this set's screens report with `trackAction` (D54): names like
   *  `shift.filter`, `[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*){1,2}`, at most 64 bytes, unique within
   *  the set. A name not declared here is dropped. */
  readonly actions?: readonly string[];
}

export interface NavEntry {
  /** The label's message key (typed; a missing key is a compile error). */
  readonly label: MessageKey;
  /** Path relative to `/<name>`. */
  readonly to: string;
  /** `main`: the primary navigation. `admin`: the "Administration" group. */
  readonly area: "main" | "admin";
  /** Who sees it before the server decides: `public` (always), `signed-in` (default), or
   *  the permissions of which the person needs any one (from session.permissions): an entry
   *  matches when the session holds `p` exactly or any `p@<department-id>`; an empty list
   *  means none, so the entry is hidden. The server still decides every call. */
  readonly access?: "public" | "signed-in" | readonly string[];
}

/** What a route's `handle` carries. */
export interface RouteHandle {
  readonly title: MessageKey;
  readonly access?: "public" | "signed-in";
}
