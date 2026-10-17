import { useRichT } from "@/i18n";
import type { FieldMessage } from "./fieldMessage";

/** A field message as the person reads it. In the `field.unknown` case the server's English
 *  `reason` sits inside a `<span lang="en">` within the translated wrapper (decision 2). */
export function FieldMessageText({ message }: { message: FieldMessage }) {
  const rich = useRichT();
  if (message.reason === undefined) return <>{message.text}</>;
  return <>{rich("field.unknown", { reason: message.reason })}</>;
}
