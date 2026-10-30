import { useEffect, useMemo, useRef } from "react";
import type { RefObject } from "react";
import { TriangleAlert } from "lucide-react";
import type { ApiError } from "@/api";
import { useT } from "@/i18n";
import { FieldMessageText } from "./FieldMessageText";
import { isUntranslatedCode, messageFor, showsRef } from "./messages";
import { ReferenceLine } from "./ReferenceLine";
import { notifyUntranslated } from "./untranslated";
import { useFieldErrors } from "./useFieldErrors";

function inputFor(form: HTMLFormElement | null, name: string): HTMLElement | null {
  const el = form?.elements.namedItem(name);
  return el instanceof HTMLElement && el.id !== "" ? el : null;
}

function labelOf(input: HTMLElement): string {
  const label = input.ownerDocument.querySelector(`label[for="${CSS.escape(input.id)}"]`);
  return label?.firstChild?.textContent?.trim() ?? "";
}

/** A form's error summary: `role="alert"`, takes focus when it appears, lists each field
 *  error as a link to its input (found by the input's `name`, equal to `fields[].field`),
 *  then the non-field message; shows the reference when `showsRef(error)`. */
export function ErrorSummary({
  error,
  formRef,
  message,
}: {
  error: ApiError | null;
  formRef: RefObject<HTMLFormElement | null>;
  /** Replaces the sentence for the error's code (sign-in says "That e-mail address or password isn't right."). */
  message?: string;
}) {
  const t = useT();
  const fieldMessages = useFieldErrors(error);
  const box = useRef<HTMLDivElement>(null);
  const untranslated = error !== null && isUntranslatedCode(error);
  useEffect(() => {
    if (error) box.current?.focus();
  }, [error]);
  useEffect(() => {
    if (error && untranslated) notifyUntranslated("error", error.code, error);
  }, [error, untranslated]);
  const entries = useMemo(() => Object.entries(fieldMessages), [fieldMessages]);
  if (!error) return null;
  // A 409 whose fields all sit on inputs of this form is a refusal the person fixes in place
  // (a playa name that is `taken`): point at the fields, no reload advice. A conflict on a
  // field with no input here (a request's status) is a state clash and keeps its sentence.
  // eslint-disable-next-line react-hooks/refs -- the form is mounted before an error can exist
  const fixable = error.code === "conflict" && entries.some(([f]) => inputFor(formRef.current, f));
  const sentence = message ?? (fixable ? t("error.invalid") : messageFor(t, error));
  return (
    <div
      ref={box}
      role="alert"
      tabIndex={-1}
      className="flex flex-col items-start gap-3 rounded-md border border-danger bg-danger-bg p-4"
    >
      <p className="flex items-start gap-2 font-semibold text-danger">
        <TriangleAlert aria-hidden="true" className="mt-1 size-5 shrink-0" />
        <span>{sentence}</span>
      </p>
      {entries.length > 0 ? (
        <ul className="list-disc ps-5">
          {/* eslint-disable-next-line react-hooks/refs */}
          {entries.map(([field, message]) => {
            // The form is mounted before an error can exist, so its inputs are in the DOM now.
            const input = inputFor(formRef.current, field);
            const label = input ? labelOf(input) : "";
            const body = (
              <>
                {label ? `${label}: ` : ""}
                <FieldMessageText message={message} />
              </>
            );
            return (
              <li key={field}>
                {input ? (
                  <a
                    href={`#${input.id}`}
                    className="inline-flex min-h-11 items-center underline"
                    onClick={(e) => {
                      e.preventDefault();
                      input.focus();
                    }}
                  >
                    {body}
                  </a>
                ) : (
                  body
                )}
              </li>
            );
          })}
        </ul>
      ) : null}
      {showsRef(error) && !fixable && error.ref ? <ReferenceLine reference={error.ref} /> : null}
    </div>
  );
}
