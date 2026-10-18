import createClient from "openapi-fetch";
import type { FetchResponse, MaybeOptionalInit } from "openapi-fetch";
import type {
  HttpMethod,
  MediaType,
  PathsWithMethod,
  RequiredKeysOf,
} from "openapi-typescript-helpers";
import { ApiError, isAbort } from "./errors";
import type { Field, Method } from "./errors";
import { newTraceparent } from "./traceparent";

/** The client gives up just after the server would (http.request_timeout defaults to 30 s). */
export const TIMEOUT_MS = 35_000;
/** The session endpoint: a 401 there means signed out, not "your session ended". */
export const SESSION_ROUTE = "/api/accounts/session";
/** Routes whose 401 never sends a person to sign-in: the session check, and the sign-in call
 *  itself, where a 401 means "wrong e-mail address or password" (web-shell.md decision 6 names
 *  only the session endpoint; sign-in would otherwise redirect to itself). */
const NO_REDIRECT_ON_401 = new Set([SESSION_ROUTE, "/api/accounts/sign-in"]);

/** Hooks the frame installs once (decision 6). Consumers: the frame; tests install fakes. */
export interface ClientHooks {
  /** The frame clears the session and navigates to sign-in. */
  onUnauthenticated(returnTo: string): void;
  /** From X-Read-Only on every response. */
  onReadOnly(readOnly: boolean): void;
  /** The reporter decides whether it is reportable. */
  onFailure(error: ApiError): void;
}

const noHooks: ClientHooks = {
  onUnauthenticated() {},
  onReadOnly() {},
  onFailure() {},
};
let hooks: ClientHooks = noHooks;

/** Installs the three hooks (frame only; tests install fakes). */
export function configureClient(next: ClientHooks): void {
  hooks = next;
}

/** A hook is the frame's code; a bug in it must not change what a screen sees. */
function safely(fn: () => void): void {
  try {
    fn();
  } catch {
    // swallowed on purpose: the caller still gets the ApiError or the response
  }
}

/** Options accepted on every call. */
export interface CallOptions {
  /** The caller's cancellation; the client adds its own 35 s timeout (decision 6). */
  signal?: AbortSignal;
  /** Path and query parameters, typed from the document. */
  params?: {
    path?: Record<string, string>;
    query?: Record<string, string | number | boolean>;
  };
}

const SHELL_SEGMENTS = new Set(["modules", "admin", "client-errors", "openapi.json"]);

/** "hello" from "/api/hello/greeting"; "core" for the shell's own routes. */
export function moduleOf(route: string): string {
  const seg = route.split("/")[2] ?? "";
  if (seg === "" || SHELL_SEGMENTS.has(seg)) return "core";
  return seg;
}

interface CallInfo {
  method: Method;
  route: string;
  module: string;
  traceId: string;
  traceparent: string;
}

function parseRetryAfter(h: string | null): number | undefined {
  if (h === null) return undefined;
  const n = Number(h.trim());
  return Number.isFinite(n) && n >= 0 ? Math.floor(n) : undefined;
}

const REF = /^[0-9A-F]{4}-[0-9A-F]{4}$/;

function toFields(v: unknown): Field[] {
  if (!Array.isArray(v)) return [];
  const out: Field[] = [];
  for (const raw of v as unknown[]) {
    if (typeof raw !== "object" || raw === null) continue;
    const f = raw as Record<string, unknown>;
    if (typeof f["field"] !== "string") continue;
    const field: Field = {
      field: f["field"],
      code: typeof f["code"] === "string" ? f["code"] : "",
      reason: typeof f["reason"] === "string" ? f["reason"] : "",
    };
    const p = f["params"];
    if (typeof p === "object" && p !== null) {
      const params: Record<string, string> = {};
      for (const [k, val] of Object.entries(p)) params[k] = String(val);
      field.params = params;
    }
    out.push(field);
  }
  return out;
}

function fail(
  info: CallInfo,
  init: ConstructorParameters<typeof ApiError>[0],
  is401: boolean,
): never {
  const error = new ApiError(init);
  if (is401 && !NO_REDIRECT_ON_401.has(info.route)) {
    safely(() => hooks.onUnauthenticated(location.pathname + location.search));
  }
  safely(() => hooks.onFailure(error));
  if (import.meta.env.DEV) {
    // Development only: the whole problem, so a developer sees what the server said.
    console.debug("api error", {
      status: error.status,
      code: error.code,
      ref: error.ref,
      fields: error.fields,
      route: error.route,
    });
  }
  throw error;
}

/** One call, start to finish (the spec's "A call, step by step"). Never calls anything but fetch. */
async function perform(req: Request, info: CallInfo): Promise<Response> {
  const base = {
    method: info.method,
    route: info.route,
    module: info.module,
    traceId: info.traceId,
  };
  const headers = new Headers(req.headers);
  headers.set("traceparent", info.traceparent);
  headers.set("Accept", "application/json, application/problem+json");

  const controller = new AbortController();
  let timedOut = false;
  const timer = setTimeout(() => {
    timedOut = true;
    controller.abort();
  }, TIMEOUT_MS);
  const relay = () => controller.abort();
  req.signal.addEventListener("abort", relay);
  if (req.signal.aborted) controller.abort();

  try {
    const outgoing = new Request(req, {
      headers,
      credentials: "same-origin",
      signal: controller.signal,
    });
    let res: Response;
    try {
      res = await fetch(outgoing);
    } catch (e) {
      if (!timedOut && isAbort(e)) throw e; // the caller's own abort: not an error, not reported
      return fail(
        info,
        { ...base, status: 0, code: "network", detail: timedOut ? "timeout" : undefined },
        false,
      );
    }
    safely(() => hooks.onReadOnly(res.headers.get("X-Read-Only") === "true"));

    let text: string;
    try {
      text = res.status === 204 || res.status === 205 ? "" : await res.text();
    } catch (e) {
      if (!timedOut && isAbort(e)) throw e;
      return fail(
        info,
        { ...base, status: 0, code: "network", detail: timedOut ? "timeout" : "body" },
        false,
      );
    }

    if (res.ok) {
      // A JSON Lines export is many documents, not one: callers ask for it as text.
      const lines = (res.headers.get("Content-Type") ?? "").includes("ndjson");
      if (text.trim() !== "" && !lines) {
        try {
          JSON.parse(text);
        } catch {
          return fail(
            info,
            { ...base, status: res.status, code: "malformed_response", detail: "json" },
            false,
          );
        }
      }
      return new Response(res.status === 204 || res.status === 205 ? null : text, {
        status: res.status,
        statusText: res.statusText,
        headers: res.headers,
      });
    }

    const is401 = res.status === 401;
    const retryAfter = parseRetryAfter(res.headers.get("Retry-After"));
    if ((res.headers.get("Content-Type") ?? "").includes("application/problem+json")) {
      let body: unknown;
      try {
        body = JSON.parse(text);
      } catch {
        body = undefined;
      }
      const p = (typeof body === "object" && body !== null ? body : {}) as Record<string, unknown>;
      if (typeof p["code"] === "string" && p["code"] !== "") {
        return fail(
          info,
          {
            ...base,
            status: res.status,
            code: p["code"],
            ref: typeof p["ref"] === "string" && REF.test(p["ref"]) ? p["ref"] : undefined,
            fields: toFields(p["fields"]),
            retryAfter,
          },
          is401,
        );
      }
    }
    return fail(
      info,
      { ...base, status: res.status, code: "malformed_response", retryAfter },
      is401,
    );
  } finally {
    clearTimeout(timer);
    req.signal.removeEventListener("abort", relay);
  }
}

type InitParam<Init> =
  RequiredKeysOf<Init> extends never
    ? [(Init & { [key: string]: unknown })?]
    : [Init & { [key: string]: unknown }];

type Op<T, M extends string> = T extends { [K in M]: infer O } ? O : never;

type Body<
  Paths extends object,
  M extends HttpMethod,
  P extends PathsWithMethod<Paths, M>,
  I,
> = Exclude<
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  FetchResponse<Op<Paths[P], M> & Record<string | number, any>, I, MediaType>["data"],
  undefined
>;

type Init<T, M extends string> = M extends keyof T ? MaybeOptionalInit<T, M> : never;

type Call<Paths extends object, M extends HttpMethod> = <
  P extends PathsWithMethod<Paths, M>,
  I extends Init<Paths[P], M>,
>(
  url: P,
  ...init: InitParam<I>
) => Promise<Body<Paths, M, P, I>>;

/** Typed by the OpenAPI paths: api.GET("/api/modules"), api.POST(path, { body }). Resolves
 *  with the parsed body (undefined for 204); rejects with ApiError, never with anything else
 *  (a caller's own abort rejects with its AbortError). */
export interface Api<Paths extends object> {
  GET: Call<Paths, "get">;
  POST: Call<Paths, "post">;
  PUT: Call<Paths, "put">;
  PATCH: Call<Paths, "patch">;
  DELETE: Call<Paths, "delete">;
}

/** Builds a client over a generated `paths` type. The shell's `api` is makeApi<paths>(). */
export function makeApi<Paths extends object>(): Api<Paths> {
  const raw = createClient<Paths>({ baseUrl: location.origin });

  const make = (method: Method) => {
    const key = method;
    return async (path: string, init?: Record<string, unknown>): Promise<unknown> => {
      const trace = newTraceparent();
      const info: CallInfo = {
        method: key,
        route: path,
        module: moduleOf(path),
        traceId: trace.traceId,
        traceparent: trace.header,
      };
      // The typed surface is the generic Call<> above; inside, the path is a string.
      const call = raw[key] as unknown as (
        p: string,
        o: Record<string, unknown>,
      ) => Promise<{ data?: unknown }>;
      const res = await call(path, { ...init, fetch: (r: Request) => perform(r, info) });
      return res.data;
    };
  };

  return {
    GET: make("GET"),
    POST: make("POST"),
    PUT: make("PUT"),
    PATCH: make("PATCH"),
    DELETE: make("DELETE"),
  } as unknown as Api<Paths>;
}
