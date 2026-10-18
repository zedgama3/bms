import { useT } from "@/i18n";
import { controlClass, Field } from "@/ui";
import type { FieldMessage } from "@/ui";

/** A department's description and shared mailbox: the inputs of the new-department form and of
 *  "Edit details". `name`s equal the API's field paths so errors land on them. */
export function DetailsFields({
  description,
  contactEmail,
  errors,
}: {
  description?: string;
  contactEmail?: string;
  errors: Readonly<Record<string, FieldMessage>>;
}) {
  const t = useT();
  return (
    <>
      <Field
        label={t("core.departments.field.description")}
        name="description"
        error={errors["description"]}
      >
        {(control) => (
          <textarea
            {...control}
            className={controlClass}
            rows={4}
            autoComplete="off"
            defaultValue={description ?? ""}
          />
        )}
      </Field>
      <Field
        label={t("core.departments.field.contact_email")}
        name="contact_email"
        hint={t("core.departments.field.contact_email_hint")}
        type="email"
        inputMode="email"
        autoComplete="off"
        defaultValue={contactEmail ?? ""}
        error={errors["contact_email"]}
      />
    </>
  );
}
