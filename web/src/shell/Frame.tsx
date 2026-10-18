import { Suspense } from "react";
import { Navigate, Outlet, useLocation, useMatches, useNavigation } from "react-router";
import { useT } from "@/i18n";
import { Loading } from "@/ui";
import type { RouteHandle } from "@/registry/types";
import { CatalogueBanner, ReadOnlyBanner, SessionBanner, SuperuserBanner } from "./Banners";
import { Header } from "./Nav";
import { RouteAnnouncer } from "./RouteAnnouncer";
import { useRegionalName } from "./regional";
import { getSessionEnded, useSession } from "./session";

const PASSWORD_PATH = "/accounts/password";

/** The page: skip link, header and navigation, banners, `<main id="content">`, footer. It
 *  is also the gate: a signed-in route visited signed out redirects to sign-in before any call (F35). */
export function Frame() {
  const t = useT();
  const regional = useRegionalName();
  const session = useSession();
  const matches = useMatches();
  const location = useLocation();
  const navigation = useNavigation();

  const handle = matches[matches.length - 1]?.handle as RouteHandle | undefined;
  const needsSignIn = handle !== undefined && handle.access !== "public" && !session.signedIn;
  // A temporary password (identity decision 9): the server answers 401 to everything else, so
  // the only screen that works is the one that changes it.
  const needsPassword =
    session.signedIn &&
    session.passwordChangeRequired &&
    location.pathname !== PASSWORD_PATH &&
    !location.pathname.startsWith(`${PASSWORD_PATH}/`);

  return (
    <div className="flex min-h-dvh flex-col">
      <a
        href="#content"
        className="sr-only focus:not-sr-only focus:fixed focus:start-2 focus:top-2 focus:z-50 focus:rounded-md focus:bg-bg focus:px-4 focus:py-3 focus:font-semibold"
        onClick={(e) => {
          e.preventDefault();
          document.getElementById("content")?.focus();
        }}
      >
        {t("shell.skip")}
      </a>
      <Header />
      {/* Always mounted, so a banner that appears later is announced (a live region that
          arrives with its text is often not). */}
      <div role="status">
        <ReadOnlyBanner />
        <SuperuserBanner />
        <SessionBanner />
        <CatalogueBanner />
      </div>
      <main
        id="content"
        tabIndex={-1}
        aria-busy={navigation.state === "loading" || undefined}
        className="mx-auto w-full max-w-6xl flex-1 px-4 py-6 focus:outline-none"
      >
        {needsSignIn ? (
          <Navigate
            to={`/accounts/sign-in?returnTo=${encodeURIComponent(location.pathname + location.search)}`}
            replace
            state={{ ended: getSessionEnded() }}
          />
        ) : needsPassword ? (
          <Navigate
            to={`${PASSWORD_PATH}?returnTo=${encodeURIComponent(location.pathname + location.search)}`}
            replace
          />
        ) : (
          <Suspense fallback={<Loading />}>
            <Outlet />
          </Suspense>
        )}
      </main>
      <footer className="border-t border-border px-4 py-4">
        <div className="mx-auto flex w-full max-w-6xl flex-wrap items-center justify-between gap-2 text-sm text-muted">
          <span>{regional}</span>
          <small>{t("shell.footer.version", { version: __BMS_VERSION__ })}</small>
        </div>
      </footer>
      <RouteAnnouncer />
    </div>
  );
}
