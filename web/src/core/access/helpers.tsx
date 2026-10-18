import type { Session } from "@/api";
import { isMessageKey, useT } from "@/i18n";

/** Whether the session shows a permission, site-wide or for some department (a convenience for
 *  hiding buttons; the server decides every call). */
export function holds(session: Session, permission: string): boolean {
  return (
    session.signedIn &&
    session.permissions.some((p) => p === permission || p.startsWith(`${permission}@`))
  );
}

/** The permissions' module or area: the first segment of the name. */
export function moduleOf(permission: string): string {
  const i = permission.indexOf(".");
  return i < 0 ? permission : permission.slice(0, i);
}

/** Translators for roles, permissions, modules and levels, falling back to the raw name when
 *  a catalogue lacks the key (a module that is compiled out, a later milestone). */
export function useTitles() {
  const t = useT();
  const lookup = (key: string, fallback: string, values?: Record<string, string>): string =>
    isMessageKey(key) ? t(key, values) : fallback;
  return {
    role(role: { name: string; key?: string | null }): string {
      if (role.name) return role.name;
      return role.key ? lookup(`core.access.role.${role.key}.title`, role.key) : "";
    },
    permission(name: string): string {
      const mod = moduleOf(name);
      return lookup(`module.${mod}.permission.${name.slice(mod.length + 1)}.title`, name);
    },
    module(name: string): string {
      return lookup(`module.${name}.title`, name);
    },
    level(level: string): string {
      return lookup(`core.access.level.${level}`, level);
    },
    scope(scope: string): string {
      return scope === "department"
        ? t("core.access.scope.department")
        : t("core.access.scope.site");
    },
  };
}

export type Titles = ReturnType<typeof useTitles>;
