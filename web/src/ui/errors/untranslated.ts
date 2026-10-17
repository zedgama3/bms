import type { ApiError } from "@/api";

/** What the frame wires to the reporter: a code with no translation was shown (decision 2). */
export type UntranslatedHandler = (what: "error" | "field", code: string, error: ApiError) => void;

let handler: UntranslatedHandler = () => {};

/** The frame installs the reporter's handler once. */
export function setUntranslatedHandler(h: UntranslatedHandler): void {
  handler = h;
}

const told = new WeakMap<ApiError, Set<string>>();

/** Tells the handler once per error and code, however many components render the message. */
export function notifyUntranslated(what: "error" | "field", code: string, error: ApiError): void {
  try {
    const key = `${what}:${code}`;
    let seen = told.get(error);
    if (!seen) {
      seen = new Set();
      told.set(error, seen);
    }
    if (seen.has(key)) return;
    seen.add(key);
    handler(what, code, error);
  } catch {
    // never throws into a render
  }
}
