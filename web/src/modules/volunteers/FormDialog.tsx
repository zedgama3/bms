import { useEffect, useId, useRef } from "react";
import type { FormEvent, ReactNode } from "react";
import { useReadOnly } from "@/api";
import { useT } from "@/i18n";
import { Button, Dialog, ErrorSummary, useFieldErrors } from "@/ui";
import type { FieldMessage } from "@/ui";
import { focusSoon } from "./helpers";
import { useConflictMessage } from "./ScreenError";
import { useAction } from "./useAction";

/** A dialog holding one form: field errors land on their inputs (`name` equals the API's field
 *  path), the summary names the rest, success closes it. The fields mount only while open, so
 *  each opening starts from the `defaultValue`s given. */
export function FormDialog({
  open,
  title,
  submit,
  submitDisabled = false,
  returnFocus,
  onClose,
  onSubmit,
  children,
}: {
  open: boolean;
  title: string;
  submit: string;
  /** Holds the submit button back while what the form needs has not loaded. */
  submitDisabled?: boolean;
  /** A selector for where focus goes after a successful submit, when the opener is gone. */
  returnFocus?: string;
  onClose: () => void;
  onSubmit: (data: FormData) => Promise<void>;
  children: (errors: Readonly<Record<string, FieldMessage>>) => ReactNode;
}) {
  const t = useT();
  const { readOnly } = useReadOnly();
  const formRef = useRef<HTMLFormElement>(null);
  const readOnlyId = useId();
  const act = useAction();
  const { clear } = act;
  const errors = useFieldErrors(act.error);
  const conflict = useConflictMessage(act.error);
  useEffect(() => {
    if (!open) clear();
  }, [open, clear]);
  const handle = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const data = new FormData(e.currentTarget);
    void act
      .run(async () => {
        await onSubmit(data);
        onClose();
      })
      .then((ok) => {
        if (ok && returnFocus) focusSoon(returnFocus);
      });
  };
  return (
    <Dialog open={open} title={title} onClose={onClose}>
      {open ? (
        <form ref={formRef} onSubmit={handle} noValidate className="flex flex-col gap-4">
          <ErrorSummary
            error={act.error}
            formRef={formRef}
            {...(conflict ? { message: conflict } : {})}
          />
          {children(errors)}
          {readOnly ? (
            <p id={readOnlyId} className="text-sm font-semibold text-warn-fg">
              {t("module.volunteers.readonly")}
            </p>
          ) : null}
          <div className="flex flex-wrap gap-3">
            <Button
              type="submit"
              busy={act.busy}
              disabled={readOnly || submitDisabled}
              {...(readOnly ? { "aria-describedby": readOnlyId } : {})}
            >
              {submit}
            </Button>
            <Button variant="secondary" onClick={onClose}>
              {t("module.volunteers.cancel")}
            </Button>
          </div>
        </form>
      ) : null}
    </Dialog>
  );
}
