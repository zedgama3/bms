import { useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { Link, Navigate, useLocation, useNavigate, useSearchParams } from "react-router";
import {
  api,
  refreshSession,
  safeReturnTo,
  setDemoSignInBusy,
  setSessionEnded,
  useAction,
  useDemo,
  useDemoSignInState,
  useSession,
} from "@/api";
import { useT } from "@/i18n";
import { Banner, Button, ErrorSummary, linkButtonClass, useFieldErrors, useToast } from "@/ui";
import { Field } from "@/ui";
import { PasswordField } from "../PasswordField";
import { text } from "../text";

/** `/accounts/sign-in`: e-mail address and password. `returnTo` is a same-origin path or
 *  ignored (F34). A wrong pair is always "isn't right", never which half. */
export function SignIn() {
  const t = useT();
  const session = useSession();
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const location = useLocation();
  const toast = useToast();
  const formRef = useRef<HTMLFormElement>(null);
  const action = useAction();
  const fieldErrors = useFieldErrors(action.error);
  const returnTo = safeReturnTo(params.get("returnTo"));
  const query = params.get("returnTo") ? `?returnTo=${encodeURIComponent(returnTo)}` : "";

  const ended = (location.state as { ended?: boolean } | null)?.ended === true;
  const flagged = (location.state as { passwordReset?: boolean } | null)?.passwordReset === true;
  const [resetDone] = useState(flagged);
  const toasted = useRef(false);
  useEffect(() => {
    if (ended && !toasted.current) {
      toasted.current = true;
      setSessionEnded(false);
      toast.show(t("error.unauthenticated"));
    }
  }, [ended, toast, t]);

  // The notice is shown once: replace the history entry without the flag so Back doesn't repeat it.
  useEffect(() => {
    if (flagged)
      void navigate(
        { pathname: location.pathname, search: location.search },
        { replace: true, state: null },
      );
  }, [flagged, navigate, location.pathname, location.search]);

  // The demo banner's "Sign in as ..." buttons (the frame's seam): fill this form's own fields
  // and submit it, so errors, busy state and returnTo are the form's. A request that arrives
  // while a sign-in is running is dropped (the banner's buttons are disabled then, too).
  const demoRequest = useDemoSignInState().request;
  const demoPassword = useDemo().password;
  const handled = useRef(demoRequest?.id ?? 0);
  useEffect(() => {
    setDemoSignInBusy(action.busy);
    return () => setDemoSignInBusy(false);
  }, [action.busy]);
  useEffect(() => {
    const form = formRef.current;
    if (!demoRequest || demoRequest.id === handled.current || !form) return;
    handled.current = demoRequest.id;
    if (action.busy) return;
    const email = form.elements.namedItem("email");
    const password = form.elements.namedItem("password");
    if (!(email instanceof HTMLInputElement) || !(password instanceof HTMLInputElement)) return;
    email.value = demoRequest.email;
    password.value = demoPassword;
    form.requestSubmit();
  }, [demoRequest, demoPassword, action.busy]);

  if (session.signedIn) return <Navigate to={returnTo} replace />;

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const data = new FormData(e.currentTarget);
    void action.run(async () => {
      await api.POST("/api/accounts/sign-in", {
        body: { email: text(data.get("email")), password: text(data.get("password")) },
      });
      await refreshSession();
      await navigate(returnTo, { replace: true });
    });
  };

  return (
    <div className="mx-auto flex max-w-md flex-col gap-4">
      <h1 className="text-2xl font-bold">{t("core.accounts.signin.title")}</h1>
      {resetDone ? <Banner>{t("core.accounts.signin.reset_done")}</Banner> : null}
      <form ref={formRef} onSubmit={submit} className="flex flex-col gap-4" noValidate>
        <ErrorSummary
          error={action.error}
          formRef={formRef}
          {...(action.error?.code === "unauthenticated"
            ? { message: t("core.accounts.signin.rejected") }
            : {})}
        />
        <Field
          label={t("core.accounts.signin.email")}
          name="email"
          type="email"
          inputMode="email"
          autoComplete="username"
          required
          error={fieldErrors["email"]}
        />
        <PasswordField
          label={t("core.accounts.signin.password")}
          name="password"
          autoComplete="current-password"
          error={fieldErrors["password"]}
        />
        <Button type="submit" busy={action.busy}>
          {t("core.accounts.signin.submit")}
        </Button>
      </form>
      <Link to="/accounts/password-reset" className={linkButtonClass("secondary")}>
        {t("core.accounts.signin.forgot")}
      </Link>
      {session.signupOpen ? (
        <Link to={`/accounts/sign-up${query}`} className={linkButtonClass("secondary")}>
          {t("core.accounts.signin.signup")}
        </Link>
      ) : null}
    </div>
  );
}
