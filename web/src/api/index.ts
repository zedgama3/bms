// The client every screen uses. No `fetch` anywhere else (decision 5). This is the public
// surface; everything the frame alone may call (configureClient, makeApi, the session
// setters, the query client) is in ./internal and banned from modules and core areas.
import { makeApi } from "./client";
import type { paths } from "./schema";

export type { CallOptions } from "./client";
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

export const api = makeApi<paths>();
