import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";
import { api, isAbort } from "@/api";
import { useT } from "@/i18n";
import { Button, linkButtonClass } from "@/ui";
import { useBoot } from "./boot";
import { usePlayaName } from "./me";
import { resetSession, useSession } from "./session";

/** The person's playa name with "Sign out", or a "Sign in" link. */
export function AccountControls() {
  const t = useT();
  const session = useSession();
  const boot = useBoot();
  const client = useQueryClient();
  const playaName = usePlayaName();
  const [busy, setBusy] = useState(false);

  if (!session.signedIn) {
    if (!boot.identityAvailable) return null; // M0: nothing to sign in to; the sign-in route says so
    return (
      <Link to="/accounts/sign-in" className={linkButtonClass("secondary")}>
        {t("shell.signin")}
      </Link>
    );
  }
  const signOut = async () => {
    setBusy(true);
    try {
      await api.POST("/api/accounts/sign-out");
    } catch (e) {
      if (isAbort(e)) return;
      // A failed sign-out still ends this page's session below; the server's cookie stays until
      // it expires, which the person sees on the next call.
    }
    client.clear();
    resetSession();
    boot.leave("/");
  };
  return (
    <div className="flex flex-wrap items-center gap-2 md:flex-nowrap">
      <span className="min-w-0 break-words font-semibold">
        {playaName || t("shell.account.anonymous")}
      </span>
      <Button
        variant="secondary"
        busy={busy}
        className="shrink-0 whitespace-nowrap"
        onClick={() => void signOut()}
      >
        {t("shell.signout")}
      </Button>
    </div>
  );
}
