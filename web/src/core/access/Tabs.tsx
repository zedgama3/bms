import { NavLink } from "react-router";
import { useT } from "@/i18n";

const cls = ({ isActive }: { isActive: boolean }) =>
  `inline-flex min-h-11 items-center rounded-md border border-border px-4 font-semibold ${isActive ? "bg-surface underline underline-offset-4" : ""}`;

/** Roles and Rules are two views of one question (who may do what): a small link bar. */
export function Tabs({ showRules }: { showRules: boolean }) {
  const t = useT();
  return (
    <nav aria-label={t("core.access.tabs")}>
      <ul className="flex flex-wrap gap-2">
        <li>
          <NavLink to="/access/roles" end className={cls}>
            {t("core.access.nav.roles")}
          </NavLink>
        </li>
        {showRules ? (
          <li>
            <NavLink to="/access/rules" end className={cls}>
              {t("core.access.nav.rules")}
            </NavLink>
          </li>
        ) : null}
      </ul>
    </nav>
  );
}
