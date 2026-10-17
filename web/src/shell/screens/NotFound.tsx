import { Link } from "react-router";
import { useT } from "@/i18n";
import { linkButtonClass } from "@/ui";

/** `*`: also what a disabled module's path shows (decision 3). */
export function NotFound() {
  const t = useT();
  return (
    <div className="flex flex-col items-start gap-4">
      <h1 className="text-2xl font-bold">{t("shell.notfound.title")}</h1>
      <p>{t("shell.notfound.body")}</p>
      <Link to="/" className={linkButtonClass("primary")}>
        {t("shell.notfound.home")}
      </Link>
    </div>
  );
}
