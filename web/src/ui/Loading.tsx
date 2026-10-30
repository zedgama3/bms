import { useEffect, useState } from "react";
import { useT } from "@/i18n";

/** The loading indicator delay: a spinner that flashes is worse than none (decision 5 table). */
export const LOADING_DELAY_MS = 300;

let mounted = 0;
const listeners = new Set<() => void>();

/** How many `Loading` indicators are mounted now (the usage collector's `screen_ready` waits for none). */
export function loadingCount(): number {
  return mounted;
}

/** Calls `fn` whenever a `Loading` mounts or unmounts. Returns the unsubscribe. */
export function subscribeLoading(fn: () => void): () => void {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
}

function changed(by: number): void {
  mounted += by;
  for (const l of listeners) l();
}

/** A polite live region that says "Loading" only after 300 ms. The screen marks its own container `aria-busy` while it loads. */
export function Loading() {
  const t = useT();
  const [show, setShow] = useState(false);
  useEffect(() => {
    changed(1);
    return () => changed(-1);
  }, []);
  useEffect(() => {
    const id = setTimeout(() => setShow(true), LOADING_DELAY_MS);
    return () => clearTimeout(id);
  }, []);
  return (
    // aria-busy belongs on the region being loaded (the screen's container), not on this live
    // region, where it would stop the announcement.
    <div aria-live="polite" className="min-h-11 py-2 text-muted">
      {show ? t("shell.loading") : null}
    </div>
  );
}
