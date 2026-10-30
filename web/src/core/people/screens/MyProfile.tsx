import { useQueryClient } from "@tanstack/react-query";
import { Link, useLocation, useNavigate, useSearchParams } from "react-router";
import { ApiError, safeReturnTo, useMe, useSetMe } from "@/api";
import { useT } from "@/i18n";
import { Banner, EmptyState, Loading } from "@/ui";
import { ConsentsSection } from "../ConsentsSection";
import { ContactsSection } from "../ContactsSection";
import { ProfileForm } from "../ProfileForm";
import { ScreenError } from "../ScreenError";
import { SectionsView } from "../SectionsView";
import { YourData } from "../YourData";

/** The page the person was going to when the frame sent them here (router state `next`), only
 *  when it is a path on this site. */
function nextPath(state: unknown): string {
  const next = (state as { next?: unknown } | null)?.next;
  const safe = typeof next === "string" ? safeReturnTo(next) : "/";
  return safe === "/" ? "" : safe;
}

/** `/people/me`: the person's own profile. `?first=1` is the first-sign-in prompt for a playa
 *  name (the frame sends a session with none here once; the person may leave). */
export function MyProfile() {
  const t = useT();
  const me = useMe();
  const setMe = useSetMe();
  const client = useQueryClient();
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const location = useLocation();
  const next = nextPath(location.state);
  const first = params.get("first") === "1";
  const error = me.error instanceof ApiError ? me.error : null;
  const profile = me.data;

  if (!profile) {
    return (
      <div className="flex flex-col gap-4" aria-busy={!error || undefined}>
        <h1 className="text-2xl font-bold">{t("core.people.me.title")}</h1>
        {error ? <ScreenError error={error} onRetry={() => void me.refetch()} /> : <Loading />}
      </div>
    );
  }

  const changed = () => void client.invalidateQueries({ queryKey: ["core", "me"] });
  const empty = profile.person.playa_name === "";

  return (
    <div className="flex flex-col gap-8">
      <h1 className="text-2xl font-bold">{t("core.people.me.title")}</h1>
      {error ? <ScreenError error={error} onRetry={() => void me.refetch()} /> : null}
      {first && empty ? (
        <div id="first-prompt">
          <Banner live={false}>{t("core.people.me.first")}</Banner>
        </div>
      ) : empty ? (
        <EmptyState>{t("core.people.me.empty")}</EmptyState>
      ) : null}
      {next ? (
        <p>
          <Link to={next} replace className="inline-flex min-h-11 items-center underline">
            {t("core.people.me.continue")}
          </Link>
        </p>
      ) : null}

      <section aria-labelledby="about-h" className="flex flex-col gap-3">
        <h2 id="about-h" className="text-xl font-bold">
          {t("core.people.profile.about")}
        </h2>
        <ProfileForm
          key={profile.person.updated_at ?? "new"}
          profile={profile}
          target="me"
          focusPlayaName={first}
          onSaved={(saved) => {
            setMe(saved);
            if (first)
              void navigate("/people/me", { replace: true, state: location.state as unknown });
          }}
        />
      </section>

      {profile.policy.emergency_contacts !== "hidden" ? (
        <ContactsSection contacts={profile.contacts} target="me" canEdit onChanged={changed} />
      ) : null}

      <ConsentsSection consents={profile.consents} target="me" canEdit onChanged={changed} />

      <SectionsView sections={profile.sections} />

      {profile.can.view_roles ? (
        <p>
          <Link
            to={`/access/grants?person=${profile.person.id}`}
            className="inline-flex min-h-11 items-center underline"
          >
            {t("core.people.me.roles")}
          </Link>
        </p>
      ) : null}

      <YourData />
    </div>
  );
}
