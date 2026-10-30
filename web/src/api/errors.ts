/** api.md "Codes", mirrored exactly; a test pins the list against web/openapi.json. */
export type ProblemCode =
  | "invalid"
  | "unauthenticated"
  | "permission"
  | "cross_origin"
  | "misdirected"
  | "not_found"
  | "method_not_allowed"
  | "conflict"
  | "payload_too_large"
  | "unsupported_media_type"
  | "rate_limited"
  | "internal"
  | "unsupported"
  | "unavailable"
  | "read_only"
  | "shutting_down"
  | "timeout";

/** Codes only the client produces. */
export type ClientCode =
  | "network" // fetch rejected: offline, DNS, reset, or the client's 35 s timeout
  | "malformed_response"; // a non-2xx without a problem body, or a 2xx whose JSON does not parse

export const PROBLEM_CODES: readonly ProblemCode[] = [
  "invalid",
  "unauthenticated",
  "permission",
  "cross_origin",
  "misdirected",
  "not_found",
  "method_not_allowed",
  "conflict",
  "payload_too_large",
  "unsupported_media_type",
  "rate_limited",
  "internal",
  "unsupported",
  "unavailable",
  "read_only",
  "shutting_down",
  "timeout",
];

/** httpx.Field, mirrored. */
export interface Field {
  field: string;
  code: string;
  params?: Record<string, string>;
  reason: string;
}

/** httpx.Problem, mirrored. */
export interface Problem {
  status: number;
  code: string;
  ref?: string;
  fields?: Field[];
}

export type Method = "GET" | "POST" | "PUT" | "PATCH" | "DELETE";

/** Wire forms from model.md, mirrored here (model.md "UI"). */
export type ID = string; // canonical UUID
export type Sensitivity = "public" | "internal" | "personal" | "sensitive";
export interface TimeRange {
  start: string;
  end: string;
} // RFC 3339 instants

export interface ApiErrorInit {
  status: number;
  /** The problem body's code (any string: a server newer than the shell may send more) or a client code. */
  code: string;
  ref?: string | undefined;
  fields?: readonly Field[] | undefined;
  method: Method;
  route: string;
  module: string;
  traceId: string;
  retryAfter?: number | undefined;
  /** For tests and logs: a short, non-personal note such as "timeout". */
  detail?: string | undefined;
  /** Wall time of the call as the client measured it, in ms (reported as `duration_ms`). */
  durationMs?: number | undefined;
}

/** Every failed call. `code` is the problem body's code, or one of the two client codes. */
export class ApiError extends Error {
  readonly status: number;
  readonly code: ProblemCode | ClientCode;
  readonly ref?: string;
  readonly fields: readonly Field[];
  readonly method: Method;
  readonly route: string;
  readonly module: string;
  readonly traceId: string;
  readonly retryAfter?: number;
  readonly detail?: string;
  /** How long the call took, in whole ms; absent when unknown. */
  readonly durationMs?: number;
  /** Set once the reporter has queued this error, so a rethrow is not reported twice. */
  reported = false;

  constructor(init: ApiErrorInit) {
    // The message carries the code and nothing else: never a URL, a value or a body.
    super(`${init.code}${init.detail ? ` (${init.detail})` : ""}`);
    this.name = "ApiError";
    this.status = init.status;
    // An unknown code from a newer server is kept as sent; messageFor handles it (F11b).
    this.code = init.code as ProblemCode | ClientCode;
    if (init.ref !== undefined) this.ref = init.ref;
    this.fields = init.fields ?? [];
    this.method = init.method;
    this.route = init.route;
    this.module = init.module;
    this.traceId = init.traceId;
    if (init.retryAfter !== undefined) this.retryAfter = init.retryAfter;
    if (init.detail !== undefined) this.detail = init.detail;
    if (init.durationMs !== undefined) this.durationMs = init.durationMs;
  }
}

/** True for an AbortError the caller caused (never an ApiError, never reported). */
export function isAbort(e: unknown): boolean {
  return e instanceof DOMException && e.name === "AbortError";
}
