import { Fragment, useId, useSyncExternalStore } from "react";
import { Info } from "lucide-react";
import { Link, useLocation } from "react-router";
import { useDemo, useDemoSignInState, useSession } from "@/api";
import { requestDemoSignIn } from "@/api/internal";
import type { DemoAccount } from "@/api";
import { isMessageKey, useT } from "@/i18n";
import { Button, linkButtonClass, Table } from "@/ui";
import type { Column } from "@/ui";

const SIGN_IN_PATH = "/accounts/sign-in";
const ADMIN_AT = "department-admin@";

/** The demo notice (D53): on every screen while `useDemo().enabled`, never dismissible,
 *  `role="note"`. On the sign-in screen it also lists the shared accounts and the one shared
 *  password. Nothing is rendered when demo mode is off. */
export function DemoBanner() {
  const t = useT();
  const demo = useDemo();
  const session = useSession();
  const { pathname } = useLocation();
  const titleId = useId();
  if (!demo.enabled) return null;
  const expanded = pathname === SIGN_IN_PATH;
  return (
    <div
      role="note"
      aria-labelledby={titleId}
      className="border-b border-warn-fg bg-warn-bg px-4 py-3 text-warn-fg"
    >
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-3">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <Info aria-hidden="true" className="size-5 shrink-0" />
          <p className="min-w-0 flex-1 basis-60">
            <strong id={titleId}>{t("shell.demo.banner.title")}</strong>{" "}
            {t("shell.demo.banner.body")}
          </p>
          {session.signedIn ? (
            <Link to="/demo/mail" className={linkButtonClass("secondary")}>
              {t("shell.demo.mail_link")}
            </Link>
          ) : null}
        </div>
        {expanded ? <DemoAccounts accounts={demo.accounts} password={demo.password} /> : null}
      </div>
    </div>
  );
}

function DemoAccounts({
  accounts,
  password,
}: {
  accounts: readonly DemoAccount[];
  password: string;
}) {
  const t = useT();
  const wide = useWide();
  const signIn = useDemoSignInState();
  // The one short label of an account (D57): `core.demo.account.<key>`; the lead's text takes
  // `{departments}`, the departments of its `department-admin@<dept>` roles in order.
  const departments = (a: DemoAccount): string =>
    a.roles
      .filter((r) => r.startsWith(ADMIN_AT))
      .map((r) => {
        const dept = r.slice(ADMIN_AT.length);
        const k = `core.demo.department.${dept}`;
        return isMessageKey(k) ? t(k) : dept;
      })
      .join(", ");
  const known = (a: DemoAccount): boolean => isMessageKey(`core.demo.account.${a.key}`);
  const accountLabel = (a: DemoAccount): string => {
    const k = `core.demo.account.${a.key}`;
    if (!isMessageKey(k)) return t("shell.demo.account_unknown");
    const list = departments(a);
    // A lead without a `department-admin@` role shows just the term, not "Lead: ".
    return t(k, { departments: list, has_departments: list === "" ? "no" : "yes" });
  };

  // One line saying what the account is for (owner, 2026-10-30); none when the catalogue has none.
  const accountDescription = (a: DemoAccount): string => {
    const k = `core.demo.account.${a.key}.description`;
    if (!isMessageKey(k)) return "";
    // A term whose module is off is not defined: the text comes back with its placeholder
    // still in it, and then there is no description.
    const text = t(k);
    return /\{term_\w+\}/.test(text) ? "" : text;
  };

  // The name must differ per row: an account the catalogue doesn't know is named by its address.
  const signInName = (a: DemoAccount): string => (known(a) ? accountLabel(a) : a.email);

  const columns: Column<DemoAccount>[] = [
    {
      header: t("shell.demo.col.account"),
      rowHeader: true,
      cell: (a) => {
        const text = accountDescription(a);
        return (
          <>
            <span className="break-words">{accountLabel(a)}</span>
            {text === "" ? null : (
              <span className="mt-1 block break-words text-sm font-normal text-muted">{text}</span>
            )}
          </>
        );
      },
    },
    {
      header: t("shell.demo.col.email"),
      cell: (a) => (
        <code translate="no" className="select-all break-words">
          {/* One line where there is room; where there is not, break only after the @. */}
          {a.email.split("@").map((part, i) => (
            <Fragment key={i}>
              {i > 0 ? (
                <>
                  @<wbr />
                </>
              ) : null}
              {part}
            </Fragment>
          ))}
        </code>
      ),
    },
  ];
  // Without a password to fill in there is nothing for the button to do.
  if (password !== "")
    columns.push({
      header: t("shell.demo.col.signin"),
      cell: (a) => (
        <Button variant="secondary" busy={signIn.busy} onClick={() => requestDemoSignIn(a.email)}>
          {t("shell.demo.sign_in_as", { account: signInName(a) })}
        </Button>
      ),
    });

  const parts = (
    <>
      {password !== "" ? (
        <p className="min-w-0 break-words">{t("shell.demo.password", { password })}</p>
      ) : null}
      {accounts.length > 0 ? (
        <Table
          caption={t("shell.demo.accounts.caption")}
          columns={columns}
          rows={accounts}
          rowKey={(a) => a.key}
        />
      ) : null}
    </>
  );

  // On a phone the table would push the sign-in form some 2000 px down: it opens on a tap.
  if (!wide) {
    return (
      <details className="rounded-md border border-border bg-bg text-fg">
        <summary className="flex min-h-11 cursor-pointer items-center px-3 text-lg font-bold">
          {t("shell.demo.accounts.title")}
        </summary>
        <div className="flex flex-col gap-3 px-3 pb-3">{parts}</div>
      </details>
    );
  }
  return (
    <section
      aria-labelledby="demo-accounts-title"
      className="flex flex-col gap-3 rounded-md border border-border bg-bg p-3 text-fg"
    >
      <h2 id="demo-accounts-title" className="text-lg font-bold">
        {t("shell.demo.accounts.title")}
      </h2>
      {parts}
    </section>
  );
}

const WIDE = "(min-width: 640px)";

/** True from Tailwind's `sm` up. Without matchMedia (old test environments) it is wide. */
function useWide(): boolean {
  return useSyncExternalStore(
    (notify) => {
      if (typeof window.matchMedia !== "function") return () => {};
      const mq = window.matchMedia(WIDE);
      mq.addEventListener("change", notify);
      return () => mq.removeEventListener("change", notify);
    },
    () => typeof window.matchMedia !== "function" || window.matchMedia(WIDE).matches,
  );
}
