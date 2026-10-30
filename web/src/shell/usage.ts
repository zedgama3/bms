// The usage collector (telemetry.md Amendment T1, D54): how the shell is used and how fast it
// is, as counts and timings only. A screen template, an action name, a number: never a person,
// a visit, a record, free text or a URL's query. Delivered to POST /api/client-events in
// batches; every path is wrapped, because a bug here must never reach a screen.
import { actionSink } from "@/usage/internal";
import { BACKOFF_MAX_MS, BACKOFF_START_MS, deliver, toPattern } from "./report";
import type { SendResult } from "./report";

const ENDPOINT = "/api/client-events";

export const USAGE_FLUSH_MS = 10_000;
/** Events held at once, and the most in one batch (the server's MaxEvents). */
export const USAGE_MAX_PENDING = 50;
export const ENTRY = "_entry";
/** The server refuses a batch holding a timing outside (0, 120000] ms or an event older than
 *  24 h; the collector drops such events itself rather than lose the whole batch (F54 to F62). */
export const MAX_VALUE_MS = 120_000;
export const MAX_CLS = 100;
export const MAX_AGE_MS = 23 * 3600_000;

export type UsageKind = "screen_view" | "action" | "screen_ready" | "vital";

export interface UsageEvent {
  kind: UsageKind;
  screen: string;
  from?: string;
  action?: string;
  name?: "lcp" | "inp" | "cls";
  value?: number;
  occurred_at: string;
  count?: number;
  app_version?: string;
}

export interface UsageBatch {
  events: UsageEvent[];
}

/** Delivers one batch; `beacon` is used while the page is being hidden. */
export type UsageSender = (batch: UsageBatch, via: "fetch" | "beacon") => Promise<SendResult>;

export interface Usage {
  /** The router settled on a screen: records `screen_view` with `from` the previous one. */
  view(pattern: string): void;
  /** Milliseconds from the navigation to the first idle moment with no `Loading` mounted. */
  ready(pattern: string, ms: number): void;
  /** A declared action on the current screen. Undeclared names are dropped. */
  action(name: string): void;
  /** Sends what is pending now. Never throws. */
  flush(reason: "timer" | "pagehide" | "manual"): void;
  /** True once the server answered 404 or 501: nothing more is sent for this page load. */
  stopped(): boolean;
  /** The pattern of the last settled screen, if any. */
  current(): string | undefined;
}

/** True when the browser asks not to be counted: Global Privacy Control or Do Not Track (T1-f). */
export function usageOff(): boolean {
  try {
    const nav = navigator as Navigator & { globalPrivacyControl?: boolean; doNotTrack?: string };
    const win = window as Window & { doNotTrack?: string };
    return nav.globalPrivacyControl === true || nav.doNotTrack === "1" || win.doNotTrack === "1";
  } catch {
    return false;
  }
}

const TOKEN = /^[A-Za-z0-9/_][A-Za-z0-9._:/{}-]{0,127}$/;

let installed: { usage: Usage; dispose: () => void } | null = null;

/** The collector installed by main.tsx, if any (none under GPC or DNT). */
export function getUsage(): Usage | null {
  return installed?.usage ?? null;
}

/** Tests only: removes the listeners and forgets the collector. */
export function uninstallUsageForTest(): void {
  installed?.dispose();
  installed = null;
}

export interface UsageOptions {
  appVersion: string;
  /** Every action name the compiled-in sets declared. */
  actions: ReadonlySet<string>;
  now?: () => Date;
  send?: UsageSender;
  /** Tests: skip the Web Vitals observers. */
  vitals?: boolean;
}

/** Installs the collector, or returns null when usage is off for this browser. Called once by
 *  main.tsx after the reporter. Idempotent. */
export function installUsage(opts: UsageOptions): Usage | null {
  if (installed) return installed.usage;
  if (usageOff()) return null;
  const now = opts.now ?? (() => new Date());
  const send: UsageSender = opts.send ?? ((batch, via) => deliver(ENDPOINT, batch, via));
  const appVersion =
    TOKEN.test(opts.appVersion) && opts.appVersion.length <= 64 ? opts.appVersion : "";

  const pending = new Map<string, UsageEvent>();
  let serial = 0;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let pausedUntil = 0;
  let backoff = 0;
  let inFlight = false;
  let off = false;
  let last: string | undefined;

  const arm = (delay: number) => {
    if (off || timer !== undefined || pending.size === 0) return;
    timer = setTimeout(() => {
      timer = undefined;
      flush("timer");
    }, delay);
  };

  const queue = (key: string | null, e: Omit<UsageEvent, "occurred_at" | "app_version">) => {
    try {
      if (off) return;
      const k = key ?? `#${++serial}`;
      const have = pending.get(k);
      if (have) {
        have.count = Math.min(1000, (have.count ?? 1) + 1);
        return;
      }
      if (pending.size >= USAGE_MAX_PENDING) {
        const oldest = pending.keys().next();
        if (!oldest.done) pending.delete(oldest.value);
      }
      pending.set(k, {
        ...e,
        occurred_at: now().toISOString(),
        ...(appVersion ? { app_version: appVersion } : {}),
      });
      arm(Math.max(USAGE_FLUSH_MS, pausedUntil - now().getTime()));
    } catch {
      // swallowed: counting is never worth a crash
    }
  };

  const flush = (reason: "timer" | "pagehide" | "manual"): void => {
    try {
      if (off || pending.size === 0 || inFlight) return;
      if (now().getTime() < pausedUntil) {
        arm(pausedUntil - now().getTime());
        return;
      }
      if (timer !== undefined) {
        clearTimeout(timer);
        timer = undefined;
      }
      // An event held through a long back-off may be too old for the server: drop it.
      const cutoff = now().getTime() - MAX_AGE_MS;
      for (const [k, e] of pending) if (Date.parse(e.occurred_at) < cutoff) pending.delete(k);
      if (pending.size === 0) return;
      const keys = [...pending.keys()].slice(0, USAGE_MAX_PENDING);
      const events = keys.map((k) => ({ ...(pending.get(k) as UsageEvent) }));
      inFlight = true;
      let result: Promise<SendResult>;
      try {
        result = send({ events }, reason === "pagehide" ? "beacon" : "fetch");
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
        case "rejected":
          // F50: the route is absent (404) or counting is off (501): stop for this page load.
          if (res.status === 404 || res.status === 501) {
            stop();
            return;
          }
          for (const k of keys) pending.delete(k); // a shell bug; this batch is dropped
          break;
        case "rate_limited":
          pausedUntil = now().getTime() + res.retryAfterS * 1000;
          break;
        case "network":
          backoff = backoff === 0 ? BACKOFF_START_MS : Math.min(BACKOFF_MAX_MS, backoff * 2);
          pausedUntil = now().getTime() + backoff;
          break;
      }
      arm(Math.max(USAGE_FLUSH_MS, pausedUntil - now().getTime()));
    } catch {
      // swallowed
    }
  };

  const stop = (): void => {
    off = true;
    pending.clear();
    if (timer !== undefined) clearTimeout(timer);
    timer = undefined;
    actionSink.current = null;
  };

  const pattern = (p: string): string | undefined => {
    const out = toPattern(p);
    return out && TOKEN.test(out) ? out : undefined;
  };

  const usage: Usage = {
    view(p) {
      const screen = pattern(p);
      if (!screen) return;
      const from = last ?? ENTRY;
      last = screen;
      queue(`screen_view\u0000${screen}\u0000${from}\u0000`, { kind: "screen_view", screen, from });
    },
    ready(p, ms) {
      const screen = pattern(p);
      if (!screen || !Number.isFinite(ms) || ms <= 0 || ms > MAX_VALUE_MS) return;
      queue(null, { kind: "screen_ready", screen, value: Math.max(1, Math.round(ms)) });
    },
    action(name) {
      if (!opts.actions.has(name)) {
        // F49: dropped; a developer sees why
        if (import.meta.env.DEV)
          console.warn(`usage: action "${name}" is not declared in a ScreenSet`);
        return;
      }
      if (last === undefined) return;
      queue(`action\u0000${last}\u0000\u0000${name}`, {
        kind: "action",
        screen: last,
        action: name,
      });
    },
    flush,
    stopped: () => off,
    current: () => last,
  };

  const vital = (name: "lcp" | "inp" | "cls", value: number) => {
    if (last === undefined || !Number.isFinite(value) || value < 0) return;
    if (name === "cls" ? value > MAX_CLS : value <= 0 || value > MAX_VALUE_MS) return;
    queue(null, {
      kind: "vital",
      screen: last,
      name,
      value: name === "cls" ? value : Math.max(1, Math.round(value)),
    });
  };

  actionSink.current = (name) => usage.action(name);

  const onHide = () => flush("pagehide");
  // Phones often never fire pagehide: send at once when hidden, then once more for the Web
  // Vitals callbacks that run on the same event after this listener.
  const onVisibility = () => {
    if (document.visibilityState !== "hidden") return;
    flush("pagehide");
    setTimeout(() => flush("pagehide"), 0);
  };
  window.addEventListener("pagehide", onHide);
  document.addEventListener("visibilitychange", onVisibility);

  if (opts.vitals !== false) {
    import("web-vitals").then(
      (v) => {
        if (off) return;
        v.onLCP((m) => vital("lcp", m.value));
        v.onINP((m) => vital("inp", m.value));
        v.onCLS((m) => vital("cls", m.value));
      },
      () => {
        // the library is optional measurement; a failed chunk is not worth a report
      },
    );
  }

  installed = {
    usage,
    dispose() {
      window.removeEventListener("pagehide", onHide);
      document.removeEventListener("visibilitychange", onVisibility);
      if (timer !== undefined) clearTimeout(timer);
      actionSink.current = null;
      off = true;
    },
  };
  return usage;
}
