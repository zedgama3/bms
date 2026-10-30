import { useEffect, useId, useRef, useState } from "react";
import { Menu, X } from "lucide-react";
import { Link, NavLink, useLocation } from "react-router";
import { usePublic } from "@/api";
import { useT } from "@/i18n";
import { useRegionalName } from "./regional";
import { navItems } from "./registryNav";
import { useSession } from "./session";
import { useEnabledSets } from "./sets";
import { LanguageMenu } from "./LanguageMenu";
import { AccountControls } from "./AccountControls";

const linkClass = ({ isActive }: { isActive: boolean }) =>
  `inline-flex min-h-11 items-center rounded-md px-3 py-2 font-semibold md:whitespace-nowrap ${isActive ? "bg-surface underline underline-offset-4" : "hover:bg-surface"}`;

/** The home link: the logo when the regional has one (its alt is the regional's name, so the
 *  link's name is the same as with text), else, or when the image will not load (a copy
 *  without the bytes answers 503), the name as text. */
function HomeLink({ name }: { name: string }) {
  const { logo } = usePublic();
  // The file ID that failed, so a replaced logo gets another try.
  const [failed, setFailed] = useState("");
  const showImage = logo !== "" && failed !== logo;
  return (
    <Link
      to="/"
      className="inline-flex min-h-11 min-w-11 items-center justify-center text-lg font-bold"
    >
      {showImage ? (
        <img
          src={`/api/settings/logo?v=${encodeURIComponent(logo)}`}
          alt={name}
          decoding="async"
          className="max-h-8 w-auto max-w-[12rem] object-contain"
          onError={() => setFailed(logo)}
        />
      ) : (
        name
      )}
    </Link>
  );
}

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
  const headerRef = useRef<HTMLElement>(null);
  // The sticky header grows when its entries wrap: keep a focused element out from under it
  // (2.4.11). Set through the CSSOM, which the CSP allows.
  useEffect(() => {
    const el = headerRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const root = document.documentElement;
    const measure = () => root.style.setProperty("--header-h", `${el.offsetHeight + 8}px`);
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => {
      ro.disconnect();
      root.style.removeProperty("--header-h");
    };
  }, []);

  const items = navItems(sets, session);
  const main = items.filter((i) => i.area === "main");
  const admin = items.filter((i) => i.area === "admin");
  const Icon = open ? X : Menu;

  return (
    <header ref={headerRef} className="sticky top-0 z-40 border-b border-border bg-bg">
      <div className="mx-auto flex w-full max-w-6xl flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2">
        <HomeLink name={regional} />
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
          className={`${open ? "flex" : "hidden"} w-full flex-col gap-3 border-t border-border pt-2 max-md:max-h-[calc(100dvh-5rem)] max-md:overflow-y-auto md:flex md:w-auto md:min-w-0 md:flex-1 md:flex-row md:flex-wrap md:items-center md:gap-x-2 md:border-t-0 md:pt-0`}
        >
          {main.length > 0 ? (
            <nav aria-label={t("shell.nav.main")}>
              <ul className="flex flex-col gap-1 md:flex-row md:flex-wrap">
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
              <ul className="flex flex-col gap-1 md:flex-row md:flex-wrap">
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
          <div className="md:ms-auto md:min-w-0 md:max-w-xs">
            <AccountControls />
          </div>
        </div>
      </div>
    </header>
  );
}
