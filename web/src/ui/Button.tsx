import type { ButtonHTMLAttributes, ReactNode, Ref } from "react";

export type ButtonVariant = "primary" | "secondary" | "danger";

const base =
  "inline-flex min-h-11 min-w-11 items-center justify-center gap-2 rounded-md border px-4 py-2 text-base font-semibold disabled:cursor-not-allowed disabled:opacity-60 aria-disabled:cursor-not-allowed aria-disabled:opacity-60";
const variants: Record<ButtonVariant, string> = {
  primary: "border-primary bg-primary text-primary-fg hover:bg-primary-hover",
  secondary: "border-border bg-bg text-fg hover:bg-surface",
  danger: "border-danger bg-bg text-danger hover:bg-danger-bg",
};

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  /** Disables the button and marks it busy: no double submit. */
  busy?: boolean;
  ref?: Ref<HTMLButtonElement>;
  children: ReactNode;
}

/** A native button at least 44 x 44 px. While `busy` it is aria-disabled and ignores clicks. `type` defaults to "button" so a stray button never submits. */
export function Button({
  variant = "primary",
  busy = false,
  type = "button",
  className = "",
  disabled,
  children,
  onClick,
  ...rest
}: ButtonProps) {
  return (
    <button
      type={type}
      className={`${base} ${variants[variant]} ${className}`}
      disabled={disabled}
      // Busy is aria-disabled, not disabled: focus stays on the button (a disabled button drops
      // focus and, with it, a screen-reader user's place), and clicks are ignored.
      aria-disabled={busy || undefined}
      onClick={(e) => {
        if (busy) {
          e.preventDefault();
          return;
        }
        onClick?.(e);
      }}
      {...rest}
    >
      {children}
    </button>
  );
}

/** The class string of a link that looks like a button (a link navigates; a button acts). */
export function linkButtonClass(variant: ButtonVariant = "primary"): string {
  return `${base} ${variants[variant]} no-underline`;
}
