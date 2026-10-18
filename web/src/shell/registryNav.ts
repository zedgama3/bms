import type { MessageKey } from "@/i18n";
import type { ScreenSet } from "@/registry/types";
import { canSee } from "./session";
import type { Session } from "./session";

/** One link in the navigation, with an absolute path. */
export interface NavItem {
  label: MessageKey;
  to: string;
  area: "main" | "admin";
}

/** The shell's own administration entry (the modules screen). */
const SHELL_ENTRIES: readonly NavItem[] = [
  { label: "shell.admin.modules.title", to: "/admin/modules", area: "admin" },
];
const SHELL_ACCESS: Record<string, readonly string[]> = { "/admin/modules": ["core.modules.read"] };

export function pathFor(setName: string, to: string): string {
  return to === "" ? `/${setName}` : `/${setName}/${to}`;
}

/** The entries of the enabled sets the person may see before the server decides. */
export function navItems(sets: readonly ScreenSet[], session: Session): NavItem[] {
  const items: NavItem[] = [];
  // A restricted session (temporary password) sees only the password screen.
  if (session.signedIn && session.passwordChangeRequired) {
    for (const s of sets)
      for (const e of s.nav) {
        const to = pathFor(s.name, e.to);
        if (to === "/accounts/password") items.push({ label: e.label, to, area: e.area });
      }
    return items;
  }
  for (const s of sets) {
    for (const e of s.nav) {
      if (canSee(session, e.access))
        items.push({ label: e.label, to: pathFor(s.name, e.to), area: e.area });
    }
  }
  for (const e of SHELL_ENTRIES) {
    if (canSee(session, SHELL_ACCESS[e.to])) items.push(e);
  }
  return items;
}
