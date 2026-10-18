import { TriangleAlert } from "lucide-react";
import { useT } from "@/i18n";

/** A screen the session does not show a permission for (a direct visit): the shell's forbidden
 *  sentence. The server still decides every call. */
export function Forbidden({ title }: { title: string }) {
  const t = useT();
  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-2xl font-bold">{title}</h1>
      <div
        role="alert"
        className="flex items-start gap-2 rounded-md border border-danger bg-danger-bg p-4 font-semibold text-danger"
      >
        <TriangleAlert aria-hidden="true" className="mt-1 size-5 shrink-0" />
        <p>{t("shell.forbidden")}</p>
      </div>
    </div>
  );
}
