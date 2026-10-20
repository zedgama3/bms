import { isMessageKey, useT } from "@/i18n";
import { splitName } from "./helpers";

/** Titles for templates and their owners, from the owner's catalogue (notify.md "UI"): a module's
 *  under `module.<owner>.template.<ident>.title`, a core area's under `core.<owner>.template.<ident>.title`.
 *  The raw name when no catalogue has one. */
export function useTemplateText() {
  const t = useT();
  const look = (...keys: string[]): string | null => {
    for (const k of keys) if (isMessageKey(k)) return t(k);
    return null;
  };
  return {
    title(name: string): string {
      const { owner, ident } = splitName(name);
      return (
        look(`module.${owner}.template.${ident}.title`, `core.${owner}.template.${ident}.title`) ??
        name
      );
    },
    owner(owner: string): string {
      return look(`module.${owner}.title`, `core.${owner}.title`) ?? owner;
    },
  };
}
