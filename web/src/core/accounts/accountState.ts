import type { components } from "@/api";
import type { MessageKey } from "@/i18n";

export type Account = components["schemas"]["AccountsAccount"];
export type AccountPage = components["schemas"]["AccountsAccountPage"];

/** The state labels an account shows, as message keys: Disabled or Active, plus Superuser and
 *  Temporary password when they apply. Text, never colour alone. */
export function stateKeys(a: Account): MessageKey[] {
  const keys: MessageKey[] = [
    a.disabled_at ? "core.accounts.state.disabled" : "core.accounts.state.active",
  ];
  if (a.superuser) keys.push("core.accounts.state.superuser");
  if (a.password_change_required) keys.push("core.accounts.state.temporary");
  return keys;
}
