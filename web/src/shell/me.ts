import { useEffect, useRef } from "react";
import { useLocation, useNavigate } from "react-router";
import { useMe } from "@/api";
import { useLanguage } from "@/i18n";
import { useSession } from "./session";

const MY_PROFILE = "/people/me";

/** The answers the playa-name prompt already acted on. Memory only (web-shell decision 11 and
 *  "Data": nothing but the language tag is stored). The server decides when to prompt
 *  (`first_prompt`, once per session, people P-W1); this only keeps a cached `true` from firing
 *  again on every route change. A refetch or a save replaces the answer with a new object. */
let acted = new WeakSet<object>();

/** Where the person was going when the prompt interrupted (router state on the prompt). */
export interface FirstPromptState {
  next?: string;
}

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
 *  the language module), and sends the person to `/people/me?first=1` when the answer says
 *  `first_prompt` (once per answer; the server decides who and when). It never blocks: a failed
 *  read leaves the page as it is. */
export function useMeEffects(): void {
  const session = useSession();
  const me = useMe();
  const { set } = useLanguage();
  const navigate = useNavigate();
  const { pathname, search, hash } = useLocation();
  const languageFor = useRef("");

  const personId = session.signedIn ? session.personId : "";
  const profile = me.data;

  useEffect(() => {
    if (!session.signedIn) languageFor.current = "";
  }, [session.signedIn]);

  useEffect(() => {
    if (!profile || personId === "") return;
    if (languageFor.current !== personId) {
      languageFor.current = personId;
      if (profile.person.language) set(profile.person.language);
    }
  }, [profile, personId, set]);

  const prompt = profile?.first_prompt === true;
  useEffect(() => {
    if (!profile || !prompt || acted.has(profile)) return;
    acted.add(profile);
    if (pathname === MY_PROFILE) return;
    const state: FirstPromptState = { next: `${pathname}${search}${hash}` };
    void navigate(`${MY_PROFILE}?first=1`, { replace: true, state });
  }, [profile, prompt, pathname, search, hash, navigate]);
}

/** Tests only: forget that the prompt ran. */
export function resetPromptForTest(): void {
  acted = new WeakSet<object>();
}
