import { useState } from "react";
import { useRichT, useT } from "@/i18n";
import { Button } from "../Button";

/** "If you ask for help, quote the reference 1A2B-3C4D." with a copy button (telemetry.md
 *  "UI"). Without `navigator.clipboard` the reference stays selectable text. */
export function ReferenceLine({ reference }: { reference: string }) {
  const t = useT();
  const rich = useRichT();
  const [copied, setCopied] = useState(false);
  const canCopy =
    typeof navigator !== "undefined" && typeof navigator.clipboard?.writeText === "function";
  const copy = () => {
    navigator.clipboard.writeText(reference).then(
      () => setCopied(true),
      () => setCopied(false),
    );
  };
  return (
    <div className="flex flex-col items-start gap-2">
      <p>
        {rich("shell.ref.line", {
          ref: reference,
          code: (chunks) => (
            <code className="select-all rounded bg-surface px-1 font-bold">{chunks}</code>
          ),
        })}
      </p>
      {canCopy ? (
        <Button variant="secondary" onClick={copy}>
          {t("shell.ref.copy")}
        </Button>
      ) : null}
      <span role="status" className="sr-only">
        {copied ? t("shell.ref.copied") : ""}
      </span>
    </div>
  );
}
