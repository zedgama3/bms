// What a screen may use of the usage collector (telemetry.md Amendment T1, D54): one function.
// The collector itself lives in the frame (shell/usage.ts) and plugs in through ./internal.
import { actionSink } from "./internal";

/** Counts one use of a declared action on the current screen: opening a dialog, using a
 *  filter or a tab, starting a form. `name` must be listed in the screen set's `actions`;
 *  anything else is dropped (with a warning in development). Takes no argument that could
 *  identify a person or a record, never throws, and does nothing when usage is off. */
export function trackAction(name: string): void {
  try {
    actionSink.current?.(name);
  } catch {
    // a bug in the collector never reaches the screen
  }
}
