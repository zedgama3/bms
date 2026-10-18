import { useId } from "react";
import type { InputHTMLAttributes, ReactNode } from "react";

/** A checkbox or radio with its label, the input itself 44 x 44 px (touch) and the label text
 *  part of the target. `hint` is a second line, tied by aria-describedby. */
export function Check({
  label,
  hint,
  type = "checkbox",
  className = "",
  "aria-describedby": describedBy,
  ...rest
}: Omit<InputHTMLAttributes<HTMLInputElement>, "type" | "children"> & {
  label: ReactNode;
  hint?: ReactNode;
  type?: "checkbox" | "radio";
}) {
  const id = useId();
  const hintId = `${id}-hint`;
  return (
    <div className={`flex items-start gap-3 ${className}`}>
      <input
        id={id}
        type={type}
        className="size-11 shrink-0 accent-primary"
        {...rest}
        {...(hint || describedBy
          ? { "aria-describedby": [describedBy, hint ? hintId : ""].filter(Boolean).join(" ") }
          : {})}
      />
      <div className="flex min-h-11 min-w-0 flex-1 flex-col justify-center py-1">
        <label htmlFor={id} className="font-semibold">
          {label}
        </label>
        {hint ? (
          <span id={hintId} className="text-sm text-muted">
            {hint}
          </span>
        ) : null}
      </div>
    </div>
  );
}
