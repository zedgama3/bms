import { useId } from "react";
import type { HTMLInputAutoCompleteAttribute, InputHTMLAttributes, ReactNode } from "react";
import { useT } from "@/i18n";
import { FieldMessageText } from "./errors/FieldMessageText";
import type { FieldMessage } from "./errors/fieldMessage";

/** What a custom control must spread to be wired to its label, hint and error. */
export interface ControlProps {
  id: string;
  name: string;
  required?: boolean;
  "aria-invalid"?: true;
  "aria-describedby"?: string;
}

export interface FieldProps {
  label: string;
  /** Equal to the API's `fields[].field` path, so a field error lands on this input. */
  name: string;
  hint?: string;
  error?: FieldMessage | undefined;
  /** `alert` when the error is raised by the control itself (not a submit), so it is announced. */
  errorRole?: "alert";
  required?: boolean;
  type?: InputHTMLAttributes<HTMLInputElement>["type"];
  inputMode?: InputHTMLAttributes<HTMLInputElement>["inputMode"];
  autoComplete?: HTMLInputAutoCompleteAttribute;
  maxLength?: number;
  value?: string;
  defaultValue?: string;
  onChange?: InputHTMLAttributes<HTMLInputElement>["onChange"];
  /** For a control that is not a text input (a select, a textarea). */
  children?: (control: ControlProps) => ReactNode;
}

/** The class every text-like control carries (input, select, textarea): 44 px tall, bordered. */
export const controlClass =
  "block min-h-11 w-full rounded-md border border-border bg-bg px-3 py-2 text-base text-fg aria-[invalid=true]:border-danger";

/** A label, an optional hint, the control and its error, tied together (WCAG 1.3.1, 3.3.1,
 *  3.3.2). Required is said in text, not colour. */
export function Field(props: FieldProps) {
  const t = useT();
  const id = useId();
  const hintId = `${id}-hint`;
  const errId = `${id}-err`;
  const describedBy = [props.hint ? hintId : "", props.error ? errId : ""]
    .filter(Boolean)
    .join(" ");
  const control: ControlProps = {
    id,
    name: props.name,
    ...(props.required ? { required: true } : {}),
    ...(props.error ? { "aria-invalid": true as const } : {}),
    ...(describedBy ? { "aria-describedby": describedBy } : {}),
  };
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="font-semibold">
        {props.label}
        {props.required ? (
          <span className="ms-1 font-normal text-muted">({t("shell.required")})</span>
        ) : null}
      </label>
      {props.hint ? (
        <p id={hintId} className="text-sm text-muted">
          {props.hint}
        </p>
      ) : null}
      {props.children ? (
        props.children(control)
      ) : (
        <input
          {...control}
          className={controlClass}
          type={props.type ?? "text"}
          {...(props.inputMode ? { inputMode: props.inputMode } : {})}
          {...(props.autoComplete ? { autoComplete: props.autoComplete } : {})}
          {...(props.maxLength ? { maxLength: props.maxLength } : {})}
          {...(props.value !== undefined ? { value: props.value } : {})}
          {...(props.defaultValue !== undefined ? { defaultValue: props.defaultValue } : {})}
          {...(props.onChange ? { onChange: props.onChange } : {})}
        />
      )}
      {props.error ? (
        <p
          id={errId}
          {...(props.errorRole ? { role: props.errorRole } : {})}
          className="text-sm font-semibold text-danger"
        >
          <span aria-hidden="true">! </span>
          <FieldMessageText message={props.error} />
        </p>
      ) : null}
    </div>
  );
}
