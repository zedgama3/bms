import type { ChangeEvent } from "react";
import { useT } from "@/i18n";
import { Check } from "@/ui";

/** The **Never expires** switch of a record or verify form: checked, the end date is cleared and
 *  disabled and the form sends `no_expiry: true` (V1.9). `name` equals the API's field path. */
export function NeverExpires({
  checked,
  onChange,
}: {
  checked: boolean;
  onChange: (e: ChangeEvent<HTMLInputElement>) => void;
}) {
  const t = useT();
  return (
    <Check
      name="no_expiry"
      checked={checked}
      onChange={onChange}
      label={t("module.volunteers.label.never_expires")}
      hint={t("module.volunteers.label.never_expires_hint")}
    />
  );
}
