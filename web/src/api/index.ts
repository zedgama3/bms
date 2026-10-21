// The client every screen uses. No `fetch` anywhere else (decision 5). This is the public
// surface; everything the frame alone may call (configureClient, makeApi, the session
// setters, the query client) is in ./internal and banned from modules and core areas.
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback } from "react";
import { makeApi } from "./client";
import { publicStore } from "./public";
import type { components, paths } from "./schema";
import { useSession } from "./session";

export { upload } from "./client";
export type { CallOptions, UploadBody } from "./client";
export { ApiError, PROBLEM_CODES, isAbort } from "./errors";
export type {
  ClientCode,
  Field,
  ID,
  Method,
  Problem,
  ProblemCode,
  Sensitivity,
  TimeRange,
} from "./errors";
export type { paths, components } from "./schema";
export { useReadOnly } from "./readonly";
export type { ReadOnlyState } from "./readonly";
export {
  refreshSession,
  safeReturnTo,
  setSessionEnded,
  useSession,
  useSessionCheckFailed,
} from "./session";
export type { Session } from "./session";

/** The public settings document's parts a screen may need, kept by the frame (`applyPublic`):
 *  the regional's name and the logo's file ID (`""` when there is no logo). */
export const usePublic = publicStore.use;
export type { PublicState } from "./public";

export const api = makeApi<paths>();

/** The signed-in person's own profile (`GET /api/people/me`), read once per session by the frame
 *  (language, the playa-name prompt, the header) and by the people screens. Memory only. Not
 *  enabled while signed out or while a temporary password restricts the session (the server
 *  answers 401 to everything but the password screen then). ESLint limits it to core/people. */
export type Me = components["schemas"]["PeopleProfile"];

export function useMe() {
  const session = useSession();
  const personId = session.signedIn ? session.personId : "";
  return useQuery({
    queryKey: ["core", "me", personId],
    enabled: session.signedIn && !session.passwordChangeRequired,
    queryFn: ({ signal }) => api.GET("/api/people/me", { signal }),
  });
}

/** Replaces the cached own profile with a fresh answer (a save), so the header and the
 *  profile screen show it at once. ESLint limits it to core/people. */
export function useSetMe(): (profile: Me) => void {
  const client = useQueryClient();
  const session = useSession();
  const personId = session.signedIn ? session.personId : "";
  return useCallback(
    (profile: Me) => client.setQueryData(["core", "me", personId], profile),
    [client, personId],
  );
}
