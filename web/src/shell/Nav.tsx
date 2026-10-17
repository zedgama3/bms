import { useId, useState } from "react";
import { Menu, X } from "lucide-react";
import { Link, NavLink, useLocation } from "react-router";
import { useT } from "@/i18n";
import { useRegionalName } from "./regional";
import { navItems } from "./registryNav";
import { useSession } from "./session";
import { useEnabledSets } from "./sets";
import { LanguageMenu } from "./LanguageMenu";
import { AccountControls } from "./AccountControls";

const linkClass = ({ isActive }: { isActive: boolean }) =>
  `inline-flex min-h-11 items-center rounded-md px-3 py-2 font-semibold ${isActive ? "bg-surface underline underline-offset-4" : "hover:bg-surface"}`;

/** The header: the name (home link), the language menu, the person, and the navigation. On a
 *  phone the navigation is a disclosure "Menu" button opening a panel (aria-expanded); from
 *  768 px it is a horizontal bar. */
export function Header() {
  const t = useT();
  const regional = useRegionalName();
  const session = useSession();
  const sets = useEnabledSets();
  // The panel is open for one path only: navigating closes it without an effect.
  const [openAt, setOpenAt] = useState<string | null>(null);
  const panelId = useId();
  const location = useLocation();
  const open = openAt === location.pathname;

  const items = navItems(sets, session);
  const main = items.filter((i) => i.area === "main");
  const admin = items.filter((i) => i.area === "admin");
  const Icon = open ? X : Menu;

  return (
    <header className="sticky top-0 z-40 border-b border-border bg-bg">
      <div className="mx-auto flex w-full max-w-6xl flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2">
        <Link to="/" className="inline-flex min-h-11 items-center text-lg font-bold">
          {regional}
        </Link>
        <div className="ms-auto flex items-center gap-2">
          <LanguageMenu />
          <button
            type="button"
            aria-expanded={open}
            aria-controls={panelId}
            onClick={() => setOpenAt(open ? null : location.pathname)}
            className="inline-flex min-h-11 min-w-11 items-center justify-center gap-2 rounded-md border border-border px-3 font-semibold md:hidden"
          >
            <Icon aria-hidden="true" className="size-5" />
            {t("shell.menu")}
          </button>
        </div>
        <div
          id={panelId}
          className={`${open ? "flex" : "hidden"} w-full flex-col gap-3 border-t border-border pt-2 max-md:max-h-[calc(100dvh-5rem)] max-md:overflow-y-auto md:flex md:w-auto md:flex-1 md:flex-row md:items-center md:border-t-0 md:pt-0`}
        >
          {main.length > 0 ? (
            <nav aria-label={t("shell.nav.main")}>
              <ul className="flex flex-col gap-1 md:flex-row">
                {main.map((i) => (
                  <li key={i.to}>
                    <NavLink to={i.to} className={linkClass} end>
                      {t(i.label)}
                    </NavLink>
                  </li>
                ))}
              </ul>
            </nav>
          ) : null}
          {admin.length > 0 ? (
            <nav aria-label={t("shell.nav.admin")}>
              <ul className="flex flex-col gap-1 md:flex-row">
                {admin.map((i) => (
                  <li key={i.to}>
                    <NavLink to={i.to} className={linkClass} end>
                      {t(i.label)}
                    </NavLink>
                  </li>
                ))}
              </ul>
            </nav>
          ) : null}
          <div className="md:ms-auto">
            <AccountControls />
          </div>
        </div>
      </div>
    </header>
  );
}
