import { useRef } from "react";
import type { FormEvent } from "react";
import type { ApiError } from "@/api";
import { useT } from "@/i18n";
import { Button, Dialog, ErrorSummary } from "@/ui";

/** A confirm step for something that can't be taken back. The error of the action stays in the
 *  dialog until it is closed. */
export function ConfirmDialog(props: {
  open: boolean;
  title: string;
  body: string;
  confirm: string;
  busy: boolean;
  error: ApiError | null;
  /** Replaces the sentence for the error's code. */
  message?: string;
  onConfirm: () => void;
  onClose: () => void;
}) {
  const t = useT();
  const formRef = useRef<HTMLFormElement>(null);
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    props.onConfirm();
  };
  return (
    <Dialog open={props.open} title={props.title} onClose={props.onClose}>
      <form ref={formRef} onSubmit={submit} className="flex flex-col gap-4">
        <ErrorSummary
          error={props.error}
          formRef={formRef}
          {...(props.message !== undefined ? { message: props.message } : {})}
        />
        <p>{props.body}</p>
        <div className="flex flex-wrap gap-3">
          <Button type="submit" variant="danger" busy={props.busy}>
            {props.confirm}
          </Button>
          <Button variant="secondary" onClick={props.onClose}>
            {t("core.notify.cancel")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
