import { useState } from "react";
import { useT } from "@/i18n";
import { Button, Field } from "@/ui";
import type { FieldMessage } from "@/ui";

/** A password input with a "Show password" button (touch-friendly, no hover). `name` equals
 *  the API's field path. The value is read from the form on submit and never kept. */
export function PasswordField(props: {
  label: string;
  name: string;
  autoComplete: "current-password" | "new-password";
  hint?: string;
  error?: FieldMessage | undefined;
  /** Which password this is, so the toggle has its own name ("Show new password"). */
  kind?: "current" | "new";
}) {
  const t = useT();
  const [shown, setShown] = useState(false);
  const showKey =
    props.kind === "current"
      ? "core.accounts.password.show_current"
      : props.kind === "new"
        ? "core.accounts.password.show_new"
        : "core.accounts.password.show";
  const hideKey =
    props.kind === "current"
      ? "core.accounts.password.hide_current"
      : props.kind === "new"
        ? "core.accounts.password.hide_new"
        : "core.accounts.password.hide";
  return (
    <div className="flex flex-col gap-2">
      <Field
        label={props.label}
        name={props.name}
        type={shown ? "text" : "password"}
        autoComplete={props.autoComplete}
        required
        {...(props.hint ? { hint: props.hint } : {})}
        error={props.error}
      />
      <div>
        <Button variant="secondary" onClick={() => setShown((s) => !s)}>
          {shown ? t(hideKey) : t(showKey)}
        </Button>
      </div>
    </div>
  );
}
