import { useEffect, useRef, useState } from "react";
import type { ChangeEvent } from "react";
import { useFormat, useT } from "@/i18n";
import { Field } from "./Field";
import type { FieldMessage } from "./errors/fieldMessage";

export interface FileFieldProps {
  label: string;
  /** Equal to the API's `fields[].field` path, so a field error lands on this input. */
  name: string;
  hint?: string;
  error?: FieldMessage | undefined;
  /** MIME types the input offers (`accept`). The browser's `File.type` is only a hint: a type
   *  outside the list is passed on and the server's sniff decides. */
  accept: readonly string[];
  /** Opens the camera on a phone where the browser supports it. */
  capture?: "environment" | "user";
  /** A larger file is refused here, before any request (the server enforces it anyway). */
  maxBytes?: number;
  value: File | null;
  onChange: (file: File | null) => void;
  disabled?: boolean;
}

/** A file picker with a visible label, hint and error like `Field`, the chosen file's name and
 *  size, and a 44 px target. Touch alone is enough: the native picker is the only control. */
export function FileField(props: FileFieldProps) {
  const t = useT();
  const format = useFormat();
  const input = useRef<HTMLInputElement>(null);
  const [tooBig, setTooBig] = useState<string | null>(null);
  const { value } = props;

  // The input cannot be controlled: when the parent clears the value, clear the input too.
  useEffect(() => {
    if (value === null && input.current) input.current.value = "";
  }, [value]);

  const pick = (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0] ?? null;
    if (file !== null && props.maxBytes !== undefined && file.size > props.maxBytes) {
      e.target.value = "";
      setTooBig(t("field.too_large", { max: format.bytes(props.maxBytes) }));
      props.onChange(null);
      return;
    }
    setTooBig(null);
    props.onChange(file);
  };

  const error: FieldMessage | undefined = tooBig !== null ? { text: tooBig } : props.error;
  return (
    <Field
      label={props.label}
      name={props.name}
      {...(props.hint ? { hint: props.hint } : {})}
      error={error}
      {...(tooBig !== null ? { errorRole: "alert" as const } : {})}
    >
      {(control) => (
        <>
          <input
            {...control}
            ref={input}
            type="file"
            accept={props.accept.join(",")}
            {...(props.capture ? { capture: props.capture } : {})}
            {...(props.disabled ? { disabled: true } : {})}
            onChange={pick}
            className="block min-h-11 w-full rounded-md border border-border bg-bg px-3 py-2 text-base text-fg file:me-3 file:min-h-11 file:rounded-md file:border file:border-border file:bg-surface file:px-4 file:font-semibold file:text-fg aria-[invalid=true]:border-danger"
          />
          {value ? (
            <p className="text-sm text-muted">
              {t("shell.file.chosen", { name: value.name, size: format.bytes(value.size) })}
            </p>
          ) : null}
        </>
      )}
    </Field>
  );
}
