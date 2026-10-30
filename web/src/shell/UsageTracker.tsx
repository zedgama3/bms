import { useEffect, useRef } from "react";
import { useLocation, useNavigation, useParams } from "react-router";
import { loadingCount, subscribeLoading } from "@/ui/internal";
import { patternOf } from "./boot";
import { getUsage } from "./usage";

/** How long a path must stand before it counts as the screen the router settled on: a
 *  redirect chain (sign-in gate, password gate) replaces the path within a few milliseconds
 *  and only its final screen is a `screen_view` (telemetry.md T1.3). */
export const SETTLE_MS = 50;

/** Feeds the usage collector: `screen_view` when the path settles, and `screen_ready` at the
 *  first moment after it that the router is idle and no `Loading` indicator is mounted.
 *  Renders nothing. A query-string change is not a new screen (and is never read). */
export function UsageTracker() {
  const { pathname } = useLocation();
  const navigation = useNavigation();
  const params = useParams();
  const idle = navigation.state === "idle";
  const idleRef = useRef(idle);
  const paramsRef = useRef(params);
  const recheck = useRef<(() => void) | null>(null);
  const navStart = useRef<number | null>(null);
  const first = useRef(true);

  useEffect(() => {
    idleRef.current = idle;
    paramsRef.current = params;
    if (!idle && navStart.current === null) navStart.current = performance.now();
    recheck.current?.();
  });

  useEffect(() => {
    const usage = getUsage();
    if (!usage) return;
    const pattern = patternOf(pathname, paramsRef.current);
    // The first screen is timed from the page's navigation start; later ones from the click.
    const started = first.current ? 0 : (navStart.current ?? performance.now());
    navStart.current = null;

    let cancelled = false;
    let verify: ReturnType<typeof setTimeout> | undefined;
    let quietAt = 0;
    const unsubscribe = { current: () => {} };

    const check = () => {
      if (cancelled) return;
      if (!idleRef.current || loadingCount() !== 0) {
        if (verify !== undefined) clearTimeout(verify);
        verify = undefined;
        return;
      }
      if (verify !== undefined) return;
      quietAt = performance.now();
      // A Loading that unmounts as the screen mounts its own is not "ready": look again.
      verify = setTimeout(() => {
        verify = undefined;
        if (cancelled || !idleRef.current || loadingCount() !== 0) return;
        cancelled = true;
        recheck.current = null;
        unsubscribe.current();
        usage.ready(pattern, quietAt - started);
      }, SETTLE_MS);
    };

    const settle = setTimeout(() => {
      if (cancelled) return;
      first.current = false;
      usage.view(pattern);
      recheck.current = check;
      unsubscribe.current = subscribeLoading(check);
      check();
    }, SETTLE_MS);

    return () => {
      cancelled = true;
      clearTimeout(settle);
      if (verify !== undefined) clearTimeout(verify);
      unsubscribe.current();
      recheck.current = null;
    };
  }, [pathname]);

  return null;
}
