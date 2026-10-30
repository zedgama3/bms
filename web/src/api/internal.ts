// Frame-private API surface (web-shell.md decision 4): modules and core areas must not
// import this file (ESLint bans "@/api/*" there).
export {
  configureClient,
  makeApi,
  moduleOf,
  SESSION_ROUTE,
  TIMEOUT_MS,
  UPLOAD_TIMEOUT_MS,
} from "./client";
export type { Api, ClientHooks } from "./client";
export { createQueryClient, retryDelay, shouldRetry } from "./query";
export {
  getSession,
  getSessionEnded,
  parseSession,
  registerSessionRefresh,
  resetSession,
  setSession,
  setSessionCheckFailed,
  signedOutLike,
  SIGNED_OUT,
  subscribeSession,
} from "./session";
export { getReadOnly, setReadOnlyState } from "./readonly";
export { publicStore, DEFAULT_PUBLIC } from "./public";
export { demoStateOf, demoStore, DEMO_OFF } from "./demo";
