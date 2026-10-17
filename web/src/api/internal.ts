// Frame-private API surface (web-shell.md decision 4): modules and core areas must not
// import this file (ESLint bans "@/api/*" there).
export { configureClient, makeApi, moduleOf, SESSION_ROUTE, TIMEOUT_MS } from "./client";
export type { Api, ClientHooks } from "./client";
export { createQueryClient, retryDelay, shouldRetry } from "./query";
export {
  getSession,
  parseSession,
  resetSession,
  setSession,
  SIGNED_OUT,
  subscribeSession,
} from "./session";
