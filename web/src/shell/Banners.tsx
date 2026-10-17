import { useT } from "@/i18n";
import { Banner, Button } from "@/ui";
import { useBoot } from "./boot";
import { useReadOnly } from "./readonly";
import { useSession } from "./session";

export function ReadOnlyBanner() {
  const t = useT();
  const { readOnly } = useReadOnly();
  return readOnly ? (
    <Banner live={false} tone="warn">
      {t("shell.banner.readonly")}
    </Banner>
  ) : null;
}

export function SuperuserBanner() {
  const t = useT();
  const session = useSession();
  return session.signedIn && session.superuser ? (
    <Banner live={false} tone="warn">
      {t("shell.banner.superuser")}
    </Banner>
  ) : null;
}

/** F29: the session check failed; the frame keeps working, signed out, with a retry. */
export function SessionBanner() {
  const t = useT();
  const boot = useBoot();
  if (!boot.sessionFailed) return null;
  return (
    <Banner
      live={false}
      actions={
        <Button variant="secondary" onClick={boot.retrySession}>
          {t("shell.retry")}
        </Button>
      }
    >
      {t("shell.banner.session")}
    </Banner>
  );
}

/** A catalogue chunk failed to load: text may show as keys until a retry succeeds. */
export function CatalogueBanner() {
  const t = useT();
  const boot = useBoot();
  if (!boot.catalogueFailed) return null;
  return (
    <Banner
      live={false}
      actions={
        <Button variant="secondary" onClick={boot.retryCatalogues}>
          {t("shell.retry")}
        </Button>
      }
    >
      {t("shell.banner.catalogue")}
    </Banner>
  );
}
