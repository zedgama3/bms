import { useEffect, useId, useRef } from "react";
import type { FormEvent } from "react";
import { useReadOnly } from "@/api";
import { useT } from "@/i18n";
import { Button, Dialog, ErrorSummary } from "@/ui";
import type { ButtonVariant } from "@/ui";
import { focusSoon } from "./helpers";
import { useConflictMessage } from "./ScreenError";
import { useAction } from "./useAction";

/** A confirmation for an action that is hard to undo: title, what will happen, the action
 *  button and Cancel. A refused action stays open with its reason (ErrorSummary); success
 *  closes it (the caller's `onConfirm` reports and refreshes). */
export function ConfirmDialog({
  open,
  title,
  body,
  confirm,
  variant = "danger",
  returnFocus,
  onClose,
  onConfirm,
}: {
  open: boolean;
  title: string;
  body: string;
  confirm: string;
  variant?: ButtonVariant;
  /** A selector for where focus goes after the action succeeds, when the opener is gone. */
  returnFocus?: string;
  onClose: () => void;
  onConfirm: () => Promise<void>;
}) {
  const t = useT();
  const { readOnly } = useReadOnly();
  const formRef = useRef<HTMLFormElement>(null);
  const readOnlyId = useId();
  const act = useAction();
  const { clear } = act;
  const conflict = useConflictMessage(act.error);
  useEffect(() => {
    if (!open) clear();
  }, [open, clear]);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    void act
      .run(async () => {
        await onConfirm();
        onClose();
      })
      .then((ok) => {
        if (ok && returnFocus) focusSoon(returnFocus);
      });
  };
  return (
    <Dialog open={open} title={title} onClose={onClose}>
      <form ref={formRef} onSubmit={submit} className="flex flex-col gap-4" noValidate>
        <ErrorSummary
          error={act.error}
          formRef={formRef}
          {...(conflict ? { message: conflict } : {})}
        />
        <p>{body}</p>
        {readOnly ? (
          <p id={readOnlyId} className="text-sm font-semibold text-warn-fg">
            {t("module.volunteers.readonly")}
          </p>
        ) : null}
        <div className="flex flex-wrap gap-3">
          <Button
            type="submit"
            variant={variant}
            busy={act.busy}
            disabled={readOnly}
            {...(readOnly ? { "aria-describedby": readOnlyId } : {})}
          >
            {confirm}
          </Button>
          <Button variant="secondary" onClick={onClose}>
            {t("module.volunteers.cancel")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
