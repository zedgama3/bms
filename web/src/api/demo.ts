// Demo mode (demo.md D53, web-shell.md amendment 2026-10-28): GET /api/demo/ is read once at
// boot beside the public settings; the frame keeps the answer here so a screen can ask
// `useDemo()` without a call of its own. Memory only.
import { createStore } from "./store";

/** One row of the sign-in page's table: roles are starter role keys, a department-scoped one as
 *  `<role>@<department key>`. */
export interface DemoAccount {
  key: string;
  email: string;
  roles: string[];
}

/** GET /api/demo/. When `enabled` is false every other field is absent. */
export interface DemoInfo {
  enabled: boolean;
  regional?: string;
  accounts?: DemoAccount[];
  password?: string;
}

/** One captured message (GET /api/demo/mail): only the signed-in account's own. */
export interface DemoMail {
  id: string;
  to: string;
  subject: string;
  body: string;
  captured_at: string;
}

export interface DemoState {
  enabled: boolean;
  accounts: readonly DemoAccount[];
  password: string;
}

export const DEMO_OFF: DemoState = { enabled: false, accounts: [], password: "" };

export const demoStore = createStore<DemoState>(DEMO_OFF);

/** Whether this is a public demo instance, and the shared accounts it offers. Off (nothing
 *  to show) until the boot read answers, and whenever the read fails. */
export const useDemo = demoStore.use;

/** A usable account row: a bad demo answer must never crash the frame that renders it. */
function isAccount(a: unknown): a is DemoAccount {
  if (typeof a !== "object" || a === null) return false;
  const r = a as Record<string, unknown>;
  return (
    typeof r["key"] === "string" &&
    typeof r["email"] === "string" &&
    Array.isArray(r["roles"]) &&
    (r["roles"] as unknown[]).every((x) => typeof x === "string")
  );
}

/** The state a `GET /api/demo/` answer means. A malformed answer is "off". */
export function demoStateOf(info: DemoInfo | undefined): DemoState {
  if (!info || info.enabled !== true) return DEMO_OFF;
  return {
    enabled: true,
    accounts: Array.isArray(info.accounts) ? info.accounts.filter(isAccount) : [],
    password: typeof info.password === "string" ? info.password : "",
  };
}

/** The two demo routes as openapi-typescript would describe them (demo.md "Interface"). The
 *  Go side documents them in the OpenAPI document only when it is built with
 *  BMS_DEMO_ENABLED, so the committed `web/openapi.json` never lists them and these
 *  hand-typed paths stay. */
interface Route<Body> {
  parameters: { query?: never; header?: never; path?: never; cookie?: never };
  get: {
    parameters: { query?: never; header?: never; path?: never; cookie?: never };
    requestBody?: never;
    responses: {
      200: { headers: { [name: string]: unknown }; content: { "application/json": Body } };
    };
  };
  put?: never;
  post?: never;
  delete?: never;
  options?: never;
  head?: never;
  patch?: never;
  trace?: never;
}

export interface DemoPaths {
  "/api/demo/": Route<DemoInfo>;
  "/api/demo/mail": Route<{ mail: DemoMail[] }>;
}
