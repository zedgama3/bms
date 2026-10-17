// The reporter (D20, telemetry.md Proposed change 4): collects what broke in the browser and
// delivers it to POST /api/client-errors within intake's limits. With the API client, the
// only code that talks to the network (decision 5). Every path is wrapped: a bug here must
// never take the page down or recurse (decision 13).
import { ApiError } from "@/api";

const ENDPOINT = "/api/client-errors";

export const FLUSH_MS = 10_000;
export const MAX_PENDING = 100;
export const MAX_COUNT = 1000;
export const MAX_BATCH = 20;
export const MAX_MESSAGE_BYTES = 512;
export const MAX_STACK_BYTES = 4096;
export const BACKOFF_START_MS = 10_000;
export const BACKOFF_MAX_MS = 5 * 60_000;
export const DEFAULT_PAUSE_S = 60;

/** Decision 14: the failures that are the shell's or the deployment's business. */
const REPORTABLE = new Set([
  "network",
  "malformed_response",
  "internal",
  "cross_origin",
  "misdirected",
  "method_not_allowed",
  "payload_too_large",
  "unsupported_media_type",
]);

const TOKEN = /^[A-Za-z0-9/_][A-Za-z0-9._:/{}-]{0,127}$/;

export type ReportKind = "crash" | "rejection" | "screen" | "api";

export interface WireReport {
  kind: ReportKind;
  message: string;
  stack?: string;
  screen?: string;
  occurred_at: string;
  count: number;
  app_version?: string;
  error_ref?: string;
  trace_id?: string;
  api?: { method: string; route: string; status: number; code: string };
}

export interface Batch {
  reports: WireReport[];
}

export type SendResult =
  | { kind: "accepted" }
  | { kind: "rate_limited"; retryAfterS: number }
  | { kind: "rejected" }
  | { kind: "network" };

/** Delivers one batch. `beacon` is used while the page is being hidden. */
export type Sender = (batch: Batch, via: "fetch" | "beacon") => Promise<SendResult>;

export interface Reporter {
  /** From the client's onFailure hook: queues an `api` report when reportable(error). */
  apiFailure(error: ApiError): void;
  /** From an ErrorBoundary: queues a `screen` report with the matched route pattern. */
  screenCrash(error: unknown, screen: string): void;
  /** A code with no translation was shown (decision 2): one `api` report per page per code. */
  untranslated(what: "error" | "field", code: string, error: ApiError): void;
  /** Sends what is pending now (pagehide, tests). Never throws. */
  flush(reason: "timer" | "pagehide" | "manual"): void;
  /** Which failures are the shell's or the deployment's business (decision 14). */
  reportable(error: ApiError): boolean;
}

function utf8Cut(s: string, max: number): string {
  const enc = new TextEncoder();
  const bytes = enc.encode(s);
  if (bytes.length <= max) return s;
  return new TextDecoder().decode(bytes.slice(0, max)).replace(/�+$/, "");
}

/** Strips every `?query` (and `#fragment`) from URLs inside a message or a stack, then cuts to max bytes. */
export function scrub(s: string, max: number): string {
  return utf8Cut(s.replace(/\?[^\s)"'<>]*/g, "").replace(/#[^\s)"'<>:]*/g, ""), max);
}

/** `/people/:id` becomes `/people/{id}`. Returns undefined when the result is not a token. */
export function toPattern(path: string): string | undefined {
  const p = path.replace(/:([A-Za-z0-9_]+)/g, "{$1}").replace(/\*/g, "{rest}");
  return TOKEN.test(p) ? p : undefined;
}

let currentScreen: string | undefined;

/** The matched route pattern the frame last saw (`:id` becomes `{id}`); undefined before the router mounted. */
export function setCurrentScreen(pattern: string | undefined): void {
  currentScreen = pattern === undefined ? undefined : toPattern(pattern);
}

/** The production sender: keepalive fetch, or sendBeacon while the page is going away. */
export const defaultSender: Sender = async (batch, via) => {
  const body = JSON.stringify(batch);
  if (via === "beacon" && typeof navigator.sendBeacon === "function") {
    if (navigator.sendBeacon(ENDPOINT, new Blob([body], { type: "application/json" }))) {
      return { kind: "accepted" };
    }
  }
  try {
    const res = await fetch(ENDPOINT, {
      method: "POST",
      keepalive: true,
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body,
    });
    if (res.status === 202) return { kind: "accepted" };
    if (res.status === 429) {
      const n = Number(res.headers.get("Retry-After"));
      return {
        kind: "rate_limited",
        retryAfterS: Number.isFinite(n) && n > 0 ? n : DEFAULT_PAUSE_S,
      };
    }
    if (res.status >= 400 && res.status < 500) return { kind: "rejected" };
    return { kind: "network" }; // 5xx: keep and back off
  } catch {
    return { kind: "network" };
  }
};

interface Entry {
  report: WireReport;
}

let installed: { reporter: Reporter; dispose: () => void } | null = null;

/** The reporter installed by main.tsx, if any. */
export function getReporter(): Reporter | null {
  return installed?.reporter ?? null;
}

/** Tests only: removes the listeners and forgets the reporter. */
export function uninstallReporterForTest(): void {
  installed?.dispose();
  installed = null;
  currentScreen = undefined;
}

/** Installs the window listeners and the flush timer. Called once, first thing in main.tsx,
 *  before React renders (decision 13). Idempotent. */
export function installReporter(opts: {
  appVersion: string;
  now?: () => Date;
  send?: Sender;
}): Reporter {
  if (installed) return installed.reporter;
  const now = opts.now ?? (() => new Date());
  const send = opts.send ?? defaultSender;
  const appVersion =
    TOKEN.test(opts.appVersion) && opts.appVersion.length <= 64 ? opts.appVersion : "";

  const pending = new Map<string, Entry>();
  const untranslatedSeen = new Set<string>();
  let timer: ReturnType<typeof setTimeout> | undefined;
  let pausedUntil = 0;
  let backoff = 0;
  let inFlight = false;

  const arm = (delay: number) => {
    if (timer !== undefined || pending.size === 0) return;
    timer = setTimeout(() => {
      timer = undefined;
      flush("timer");
    }, delay);
  };

  const queue = (r: Omit<WireReport, "count" | "app_version" | "occurred_at">) => {
    try {
      const key = `${r.kind}\u0000${r.message}\u0000${r.screen ?? ""}`;
      const have = pending.get(key);
      if (have) {
        have.report.count = Math.min(MAX_COUNT, have.report.count + 1);
        return;
      }
      if (pending.size >= MAX_PENDING) {
        const oldest = pending.keys().next();
        if (!oldest.done) pending.delete(oldest.value); // F16: the oldest is dropped
      }
      const report: WireReport = {
        ...r,
        occurred_at: now().toISOString(),
        count: 1,
        ...(appVersion ? { app_version: appVersion } : {}),
      };
      pending.set(key, { report });
      arm(Math.max(FLUSH_MS, pausedUntil - now().getTime()));
    } catch {
      // F18: swallowed
    }
  };

  const withScreen = (): { screen?: string } => (currentScreen ? { screen: currentScreen } : {});

  const flush = (reason: "timer" | "pagehide" | "manual"): void => {
    try {
      if (pending.size === 0 || inFlight) return;
      if (now().getTime() < pausedUntil) {
        arm(pausedUntil - now().getTime());
        return;
      }
      if (timer !== undefined) {
        clearTimeout(timer);
        timer = undefined;
      }
      const keys = [...pending.keys()].slice(0, MAX_BATCH);
      const reports = keys.map((k) => ({ ...(pending.get(k) as Entry).report }));
      inFlight = true;
      let result: Promise<SendResult>;
      try {
        result = send({ reports }, reason === "pagehide" ? "beacon" : "fetch");
      } catch {
        result = Promise.resolve<SendResult>({ kind: "network" });
      }
      result.then(
        (res) => settle(keys, res),
        () => settle(keys, { kind: "network" }),
      );
    } catch {
      inFlight = false;
    }
  };

  const settle = (keys: string[], res: SendResult): void => {
    try {
      inFlight = false;
      switch (res.kind) {
        case "accepted":
          backoff = 0;
          for (const k of keys) pending.delete(k);
          break;
        case "rejected": // F14: a shell bug; this batch is dropped
          for (const k of keys) pending.delete(k);
          if (import.meta.env.DEV) console.warn("client-errors: the server rejected a batch");
          break;
        case "rate_limited": // F13: keep, pause for Retry-After
          pausedUntil = now().getTime() + res.retryAfterS * 1000;
          break;
        case "network": // F15: keep, back off 10 s doubling to 5 min; never reported
          backoff = backoff === 0 ? BACKOFF_START_MS : Math.min(BACKOFF_MAX_MS, backoff * 2);
          pausedUntil = now().getTime() + backoff;
          break;
      }
      arm(Math.max(FLUSH_MS, pausedUntil - now().getTime()));
    } catch {
      // swallowed
    }
  };

  const reportable = (error: ApiError): boolean =>
    REPORTABLE.has(error.code) && error.route !== ENDPOINT;

  const apiPart = (error: ApiError) => ({
    method: error.method,
    route: TOKEN.test(error.route) ? error.route : "/api/unknown",
    status: error.status,
    code: TOKEN.test(error.code) ? error.code : "unknown",
  });

  const trace = (error: ApiError) => ({
    ...(error.ref ? { error_ref: error.ref } : {}),
    ...(/^[0-9a-f]{32}$/.test(error.traceId) ? { trace_id: error.traceId } : {}),
  });

  const reporter: Reporter = {
    reportable,
    apiFailure(error) {
      try {
        if (error.reported || !reportable(error)) return;
        error.reported = true;
        const detail = error.detail ? ` ${error.detail}` : "";
        queue({
          kind: "api",
          message: scrub(
            `api ${error.code}${detail} ${error.method} ${error.route}`,
            MAX_MESSAGE_BYTES,
          ),
          ...withScreen(),
          ...trace(error),
          api: apiPart(error),
        });
      } catch {
        // swallowed
      }
    },
    screenCrash(error, screen) {
      try {
        if (error instanceof ApiError && error.reported) return; // already counted by apiFailure
        const pattern = toPattern(screen);
        const { message, stack } = describe(error);
        queue({
          kind: "screen",
          message: scrub(message, MAX_MESSAGE_BYTES),
          ...(stack ? { stack: scrub(stack, MAX_STACK_BYTES) } : {}),
          ...(pattern ? { screen: pattern } : {}),
          ...(error instanceof ApiError ? trace(error) : {}),
        });
        if (error instanceof ApiError) error.reported = true;
      } catch {
        // swallowed
      }
    },
    untranslated(what, code, error) {
      try {
        const safe = /^[A-Za-z0-9_.-]{1,40}$/.test(code) ? code : "[unreadable]";
        const key = `${what}:${safe}`;
        if (untranslatedSeen.has(key)) return;
        untranslatedSeen.add(key);
        queue({
          kind: "api",
          message: `untranslated ${what} code ${safe}`,
          ...withScreen(),
          ...trace(error),
          api: apiPart(error),
        });
      } catch {
        // swallowed
      }
    },
    flush,
  };

  const onError = (e: ErrorEvent) => {
    try {
      const err: unknown = e.error;
      queue({
        kind: "crash",
        message: scrub(e.message || describe(err).message, MAX_MESSAGE_BYTES),
        ...(err instanceof Error && err.stack ? { stack: scrub(err.stack, MAX_STACK_BYTES) } : {}),
        ...withScreen(),
      });
    } catch {
      // swallowed
    }
  };
  const onRejection = (e: PromiseRejectionEvent) => {
    try {
      const reason: unknown = e.reason;
      if (reason instanceof ApiError && reason.reported) return;
      if (isAbortLike(reason)) return;
      const { message, stack } = describe(reason);
      queue({
        kind: "rejection",
        message: scrub(message, MAX_MESSAGE_BYTES),
        ...(stack ? { stack: scrub(stack, MAX_STACK_BYTES) } : {}),
        ...withScreen(),
      });
    } catch {
      // swallowed
    }
  };
  const onHide = () => flush("pagehide");
  const onVisibility = () => {
    if (document.visibilityState === "hidden") flush("pagehide");
  };

  window.addEventListener("error", onError);
  window.addEventListener("unhandledrejection", onRejection);
  window.addEventListener("pagehide", onHide);
  document.addEventListener("visibilitychange", onVisibility);

  installed = {
    reporter,
    dispose() {
      window.removeEventListener("error", onError);
      window.removeEventListener("unhandledrejection", onRejection);
      window.removeEventListener("pagehide", onHide);
      document.removeEventListener("visibilitychange", onVisibility);
      if (timer !== undefined) clearTimeout(timer);
    },
  };
  return reporter;
}

function isAbortLike(r: unknown): boolean {
  return r instanceof DOMException && r.name === "AbortError";
}

function describe(e: unknown): { message: string; stack?: string } {
  if (e instanceof ApiError) return { message: `api ${e.code} ${e.method} ${e.route}` };
  if (e instanceof Error) {
    const out: { message: string; stack?: string } = { message: `${e.name}: ${e.message}` };
    if (e.stack) out.stack = e.stack;
    return out;
  }
  return { message: String(e) };
}
