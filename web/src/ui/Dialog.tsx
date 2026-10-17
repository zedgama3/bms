import { useEffect, useId, useRef } from "react";
import type { ReactNode } from "react";
import { useT } from "@/i18n";
import { Button } from "./Button";

/** A native `<dialog>` opened with showModal(): focus is trapped, Esc closes it and focus
 *  returns to what opened it. Full screen below 640 px. */
export function Dialog({
  open,
  title,
  onClose,
  children,
}: {
  open: boolean;
  title: string;
  onClose: () => void;
  children: ReactNode;
}) {
  const t = useT();
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) d.showModal();
    if (!open && d.open) d.close();
  }, [open]);
  return (
    <dialog
      ref={ref}
      // The native close event fires for Esc, for the button and for our own d.close() when the
      // parent sets open=false; only the first two should tell the parent.
      onClose={() => {
        if (open) onClose();
      }}
      aria-labelledby={titleId}
      className="m-0 h-dvh max-h-none w-full max-w-none bg-bg p-4 text-fg backdrop:bg-black/50 sm:m-auto sm:h-auto sm:max-h-[90dvh] sm:w-auto sm:min-w-96 sm:max-w-xl sm:rounded-lg sm:border sm:border-border"
    >
      <div className="flex items-start justify-between gap-3">
        <h2 id={titleId} className="text-xl font-bold">
          {title}
        </h2>
        <Button variant="secondary" onClick={() => ref.current?.close()}>
          {t("shell.close")}
        </Button>
      </div>
      <div className="mt-4">{children}</div>
    </dialog>
  );
}
