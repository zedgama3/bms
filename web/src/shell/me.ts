import { useEffect, useRef } from "react";
import { useLocation, useNavigate } from "react-router";
import { useMe } from "@/api";
import { useLanguage } from "@/i18n";
import { useSession } from "./session";

const MY_PROFILE = "/people/me";

/** The person the playa-name prompt already ran for. Memory only: a fresh page load (sign-out
 *  leaves the page) asks again, and so does a new sign-in after a 401 (it resets below). */
let promptedFor = "";

/** The name the header and the home page show: the profile's (fresh after a save), else the
 *  session's, else "" (the neutral label). */
export function usePlayaName(): string {
  const session = useSession();
  const me = useMe();
  if (!session.signedIn) return "";
  return me.data?.person.playa_name || session.playaName;
}

/** After a session is established the frame reads `GET /api/people/me` once (the query is
 *  cached for the session): it applies the saved language (mirrored to the one storage key by
 *  the language module), and sends a person with no playa name (never the break-glass
 *  superuser) to `/people/me?first=1` once per session. It never blocks: a failed read leaves
 *  the page as it is. */
export function useMeEffects(): void {
  const session = useSession();
  const me = useMe();
  const { set } = useLanguage();
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const languageFor = useRef("");

  const personId = session.signedIn ? session.personId : "";
  const profile = me.data;

  useEffect(() => {
    if (!session.signedIn) {
      promptedFor = "";
      languageFor.current = "";
    }
  }, [session.signedIn]);

  useEffect(() => {
    if (!profile || personId === "") return;
    if (languageFor.current !== personId) {
      languageFor.current = personId;
      if (profile.person.language) set(profile.person.language);
    }
  }, [profile, personId, set]);

  const superuser = session.signedIn && session.superuser;
  const unnamed = profile !== undefined && profile.person.playa_name === "";
  useEffect(() => {
    if (!unnamed || superuser || personId === "" || promptedFor === personId) return;
    promptedFor = personId;
    if (pathname === MY_PROFILE) return;
    void navigate(`${MY_PROFILE}?first=1`, { replace: true });
  }, [unnamed, superuser, personId, pathname, navigate]);
}
