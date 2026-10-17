import { useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { Navigate, useLocation, useNavigate, useSearchParams } from "react-router";
import { ApiError, isAbort } from "@/api";
import { useT } from "@/i18n";
import { Banner, Button, ErrorSummary, Field, useFieldErrors, useToast } from "@/ui";
import { safeReturnTo, useBoot } from "../boot";
import { identity } from "../identity";
import { setSessionEnded, useSession } from "../session";

const text = (v: FormDataEntryValue | null): string => (typeof v === "string" ? v : "");

/** `/sign-in`: the form identity.md defines (e-mail address, password). In M0 identity does
 *  not exist (F28) and the screen says so. `returnTo` is a same-origin path or ignored (F34). */
export function SignIn() {
  const t = useT();
  const session = useSession();
  const boot = useBoot();
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const location = useLocation();
  const toast = useToast();
  const formRef = useRef<HTMLFormElement>(null);
  const [error, setError] = useState<ApiError | null>(null);
  const [busy, setBusy] = useState(false);
  const fieldErrors = useFieldErrors(error);
  const returnTo = safeReturnTo(params.get("returnTo"));

  const ended = (location.state as { ended?: boolean } | null)?.ended === true;
  const toasted = useRef(false);
  useEffect(() => {
    if (ended && !toasted.current) {
      toasted.current = true;
      setSessionEnded(false);
      toast.show(t("error.unauthenticated"));
    }
  }, [ended, toast, t]);

  if (session.signedIn) return <Navigate to={returnTo} replace />;

  const submit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const data = new FormData(e.currentTarget);
    setBusy(true);
    setError(null);
    try {
      await identity.POST("/api/accounts/sign-in", {
        body: { email: text(data.get("email")), password: text(data.get("password")) },
      });
      await boot.refreshSession();
      await navigate(returnTo, { replace: true });
    } catch (err) {
      if (isAbort(err)) return;
      if (err instanceof ApiError) setError(err);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mx-auto flex max-w-md flex-col gap-4">
      <h1 className="text-2xl font-bold">{t("shell.signin.title")}</h1>
      {!boot.identityAvailable ? (
        <Banner>{t("shell.signin.unavailable")}</Banner>
      ) : (
        <form
          ref={formRef}
          onSubmit={(e) => void submit(e)}
          className="flex flex-col gap-4"
          noValidate
        >
          <ErrorSummary
            error={error}
            formRef={formRef}
            {...(error?.code === "unauthenticated" ? { message: t("shell.signin.rejected") } : {})}
          />
          <Field
            label={t("shell.signin.email")}
            name="email"
            type="email"
            inputMode="email"
            autoComplete="username"
            required
            error={fieldErrors["email"]}
          />
          <Field
            label={t("shell.signin.password")}
            name="password"
            type="password"
            autoComplete="current-password"
            required
            error={fieldErrors["password"]}
          />
          <Button type="submit" busy={busy}>
            {t("shell.signin.submit")}
          </Button>
        </form>
      )}
    </div>
  );
}
