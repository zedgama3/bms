import { Link } from "react-router";
import { useT } from "@/i18n";
import type { MessageKey } from "@/i18n";
import { EmptyState, linkButtonClass } from "@/ui";
import { useBoot } from "../boot";
import { usePlayaName } from "../me";
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
  const playaName = usePlayaName();
  const items = navItems(sets, session);
  const cards = items.filter((i) => i.area === "main");
  const adminCards = items.filter((i) => i.area === "admin");
  // "Nothing is turned on" is about modules: the accounts area is always there.
  const anyEntries = sets.some((s) => s.kind === "module" && s.nav.some((n) => n.area === "main"));

  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-2xl font-bold">
        {session.signedIn && playaName !== ""
          ? t("shell.home.hi", { playa_name: playaName })
          : t("shell.home.welcome", { regional })}
      </h1>
      {!session.signedIn && boot.identityAvailable ? (
        <div>
          <Link to="/accounts/sign-in" className={linkButtonClass("primary")}>
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
          <CardList items={cards} />
        </section>
      ) : null}
      {adminCards.length > 0 ? (
        <section aria-labelledby="home-admin" className="flex flex-col gap-3">
          <h2 id="home-admin" className="text-xl font-bold">
            {t("shell.home.admin.title")}
          </h2>
          <CardList items={adminCards} />
        </section>
      ) : null}
    </div>
  );
}

function CardList({ items }: { items: readonly { to: string; label: MessageKey }[] }) {
  const t = useT();
  return (
    <ul className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
      {items.map((c) => (
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
  );
}
