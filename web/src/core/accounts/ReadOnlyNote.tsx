import { useT } from "@/i18n";

/** Says why a write is disabled on a read-only copy. Give the id to the disabled control's
 *  aria-describedby. Renders nothing on a writing copy. */
export function ReadOnlyNote({ id, readOnly }: { id: string; readOnly: boolean }) {
  const t = useT();
  if (!readOnly) return null;
  return (
    <p id={id} className="text-sm font-semibold text-warn-fg">
      {t("core.accounts.readonly")}
    </p>
  );
}
