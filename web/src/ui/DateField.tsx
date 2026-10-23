import { controlClass, Field } from "./Field";
import type { FieldProps } from "./Field";

const DATE = /^(\d{4})-(\d{2})-(\d{2})$/;

/** A date-only form value ("2026-12-31") as the instant that ends that day in the browser's
 *  zone (local midnight at the end of it; the regional's zone arrives with settings). "" for no
 *  or a malformed date. Used for a grant's end date: the term runs through the chosen day. */
export function endOfDayInstant(date: string): string {
  const m = DATE.exec(date);
  if (!m) return "";
  const d = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]) + 1);
  return Number.isNaN(d.getTime()) ? "" : d.toISOString();
}

/** The last day an end instant covers, in the browser's zone: the instant minus 1 ms, so a
 *  term ending at local midnight shows as the day before it. */
export function lastDayOf(instant: string): Date {
  return new Date(new Date(instant).getTime() - 1);
}

/** Today's date in the browser's zone as a form value, for `min`. */
export function todayDate(now: Date = new Date()): string {
  const p = (n: number) => String(n).padStart(2, "0");
  return `${now.getFullYear()}-${p(now.getMonth() + 1)}-${p(now.getDate())}`;
}

/** A native date input (a date picker on a phone; no typing needed) inside a `Field`.
 *  Its value is a date-only string; `endOfDayInstant` turns it into the API's instant. */
export function DateField(
  props: Omit<FieldProps, "type" | "inputMode" | "autoComplete" | "children" | "maxLength"> & {
    min?: string;
  },
) {
  const { min, ...rest } = props;
  return (
    <Field {...rest}>
      {(control) => (
        <input
          {...control}
          type="date"
          className={controlClass}
          autoComplete="off"
          {...(props.disabled ? { disabled: true } : {})}
          {...(min ? { min } : {})}
          {...(props.value !== undefined ? { value: props.value } : {})}
          {...(props.defaultValue !== undefined ? { defaultValue: props.defaultValue } : {})}
          {...(props.onChange ? { onChange: props.onChange } : {})}
        />
      )}
    </Field>
  );
}
