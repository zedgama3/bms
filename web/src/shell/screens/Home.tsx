import { Link } from "react-router";
import { useT } from "@/i18n";
import { EmptyState, linkButtonClass } from "@/ui";
import { useBoot } from "../boot";
import { useRegionalName } from "../regional";
import { navItems } from "../registryNav";
import { useSession } from "../session";
import { useEnabledSets } from "../sets";

/** `/`: signed out, a welcome with a sign-in button and the public entries as cards; signed
 *  in, a greeting and a card per main entry the person may see. */
export function Home() {
  const t = useT();
  const regional = useRegionalName();
  const session = useSession();
  const sets = useEnabledSets();
  const boot = useBoot();
  const cards = navItems(sets, session).filter((i) => i.area === "main");
  const anyEntries = sets.some((s) => s.nav.some((n) => n.area === "main"));

  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-2xl font-bold">
        {session.signedIn
          ? t("shell.home.hi", { playa_name: session.playaName })
          : t("shell.home.welcome", { regional })}
      </h1>
      {!session.signedIn && boot.identityAvailable ? (
        <div>
          <Link to="/sign-in" className={linkButtonClass("primary")}>
            {t("shell.home.signin")}
          </Link>
        </div>
      ) : null}
      {!anyEntries ? (
        <EmptyState>{t("shell.home.empty")}</EmptyState>
      ) : cards.length > 0 ? (
        <section aria-labelledby="home-cards">
          <h2 id="home-cards" className="sr-only">
            {t("shell.home.cards")}
          </h2>
          <ul className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {cards.map((c) => (
              <li key={c.to}>
                <Link
                  to={c.to}
                  className="flex min-h-11 items-center rounded-md border border-border bg-surface p-4 text-lg font-semibold"
                >
                  {t(c.label)}
                </Link>
              </li>
            ))}
          </ul>
        </section>
      ) : null}
    </div>
  );
}
