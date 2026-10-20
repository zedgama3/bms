import { useEffect, useMemo } from "react";
import type { ReactNode } from "react";
import type { ApiError } from "@/api";
import { isMessageKey, useRichT, useT } from "@/i18n";
import type { FieldMessage } from "./fieldMessage";
import { notifyUntranslated } from "./untranslated";

function toText(n: ReactNode): string {
  if (typeof n === "string") return n;
  if (typeof n === "number") return String(n);
  if (Array.isArray(n)) return (n as ReactNode[]).map(toText).join("");
  return "";
}

/** Per-input messages: `fields[].code` translated through `module.<module>.field.<code>`,
 *  then `field.<code>`, with `params` as values. For a code with no translation (or an
 *  empty code) the message is the translated wrapper `field.unknown` with the server's
 *  English `reason` as its `{reason}` argument, and the code is reported once (decision 2).
 *  `reason` is set in no other case. Keys are the fields' paths. */
export function useFieldErrors(error: ApiError | null): Readonly<Record<string, FieldMessage>> {
  const t = useT();
  const rich = useRichT();
  const { messages, unknown } = useMemo(() => {
    const out: Record<string, FieldMessage> = {};
    const unknownCodes: string[] = [];
    for (const f of error?.fields ?? []) {
      if (Object.hasOwn(out, f.field)) continue; // the first problem with an input is the one shown
      const own = `module.${error?.module ?? ""}.field.${f.code}`;
      const shared = `field.${f.code}`;
      // a `url` message selects on `reason`; a server that sends none gets the general wording
      const params = f.code === "url" ? { reason: "", ...f.params } : (f.params ?? {});
      if (f.code !== "" && isMessageKey(own)) {
        out[f.field] = { text: t(own, params) };
      } else if (f.code !== "" && isMessageKey(shared)) {
        out[f.field] = { text: t(shared, params) };
      } else {
        const text = toText(rich("field.unknown", { reason: f.reason, en: (c) => c }));
        out[f.field] = { text, reason: f.reason };
        unknownCodes.push(f.code);
      }
    }
    return { messages: out, unknown: unknownCodes };
  }, [error, t, rich]);

  useEffect(() => {
    if (!error) return;
    for (const code of unknown) notifyUntranslated("field", code, error);
  }, [error, unknown]);

  return messages;
}
