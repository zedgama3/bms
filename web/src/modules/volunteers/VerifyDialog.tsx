import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useT } from "@/i18n";
import { DateField, useToast } from "@/ui";
import type { FieldMessage } from "@/ui";
import { vol } from "./calls";
import { FormDialog } from "./FormDialog";
import { text } from "./helpers";
import { NeverExpires } from "./NeverExpires";
import type { QualRecord } from "./types";

/** Verify a pending record, correcting the dates the person entered (the certificate is open
 *  in its own link). An empty date is not sent, and the service then keeps the one the record has. */
export function VerifyDialog({
  record,
  name,
  returnFocus,
  onClose,
}: {
  record: QualRecord | null;
  name: string;
  returnFocus?: string;
  onClose: () => void;
}) {
  const t = useT();
  const toast = useToast();
  const client = useQueryClient();
  return (
    <FormDialog
      open={record !== null}
      title={t("module.volunteers.verify.title", { name })}
      submit={t("module.volunteers.verify.submit")}
      onClose={onClose}
      {...(returnFocus ? { returnFocus } : {})}
      onSubmit={async (data) => {
        if (!record) return;
        const acquired = text(data.get("acquired_on")).trim();
        const expires = text(data.get("expires_on")).trim();
        const never = data.get("no_expiry") !== null;
        await vol.verifyRecord(record.id, {
          ...(acquired ? { acquired_on: acquired } : {}),
          ...(expires && !never ? { expires_on: expires } : {}),
          ...(never ? { no_expiry: true } : {}),
        });
        toast.show(t("module.volunteers.verify.done"));
        await client.invalidateQueries({ queryKey: ["volunteers"] });
      }}
    >
      {(errors) => (
        <>
          <p className="text-sm text-muted">{t("module.volunteers.verify.hint")}</p>
          <DateField
            label={t("module.volunteers.label.acquired_on")}
            name="acquired_on"
            required
            defaultValue={record?.acquired_on ?? ""}
            error={errors["acquired_on"]}
          />
          <ExpiryFields initial={record?.expires_on ?? ""} error={errors["expires_on"]} />
        </>
      )}
    </FormDialog>
  );
}

/** The end date with its **Never expires** switch: the switch clears and disables the date. */
function ExpiryFields({ initial, error }: { initial: string; error: FieldMessage | undefined }) {
  const t = useT();
  const [never, setNever] = useState(false);
  const [expires, setExpires] = useState(initial);
  return (
    <>
      <DateField
        label={t("module.volunteers.label.expires_on")}
        name="expires_on"
        hint={t(
          never
            ? "module.volunteers.label.expires_on_hint_never"
            : "module.volunteers.verify.expires_hint",
        )}
        value={never ? "" : expires}
        disabled={never}
        onChange={(e) => setExpires(e.target.value)}
        error={error}
      />
      <NeverExpires
        checked={never}
        onChange={(e) => {
          setNever(e.target.checked);
          if (e.target.checked) setExpires("");
        }}
      />
    </>
  );
}
