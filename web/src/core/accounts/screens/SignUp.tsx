import { useMemo, useRef } from "react";
import type { FormEvent } from "react";
import { Link, Navigate, useNavigate, useSearchParams } from "react-router";
import {
  api,
  ApiError,
  refreshSession,
  safeReturnTo,
  useReadOnly,
  useSession,
  useSessionCheckFailed,
} from "@/api";
import { useT } from "@/i18n";
import {
  Button,
  EmptyState,
  ErrorState,
  ErrorSummary,
  Field,
  linkButtonClass,
  useFieldErrors,
} from "@/ui";
import { DEFAULT_MIN_PASSWORD } from "../constants";
import { PasswordField } from "../PasswordField";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { text } from "../text";
import { useAction } from "../useAction";

/** `/accounts/sign-up`: e-mail address and password only (profile comes with `people`).
 *  Field errors land on the inputs by code; 409 taken, 501 invitation mode, 503 read-only and
 *  429 have their own sentences. */
export function SignUp() {
  const t = useT();
  const session = useSession();
  const { readOnly } = useReadOnly();
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const formRef = useRef<HTMLFormElement>(null);
  const action = useAction();
  const checkFailed = useSessionCheckFailed();
  // Sign-up has one possible conflict (F15): an address already in use. The server may send it
  // with or without `fields`; either way it lands on the e-mail input, without a reference line.
  const error = useMemo(() => {
    const e = action.error;
    if (e?.code !== "conflict" || e.fields.some((f) => f.field === "email")) return e;
    return new ApiError({
      status: e.status,
      code: "conflict",
      fields: [{ field: "email", code: "taken", reason: "" }],
      method: e.method,
      route: e.route,
      module: e.module,
      traceId: e.traceId,
    });
  }, [action.error]);
  const fieldErrors = useFieldErrors(error);
  const returnTo = safeReturnTo(params.get("returnTo"));
  const query = params.get("returnTo") ? `?returnTo=${encodeURIComponent(returnTo)}` : "";

  if (session.signedIn) return <Navigate to={returnTo} replace />;

  const signInLink = (
    <Link to={`/accounts/sign-in${query}`} className={linkButtonClass("secondary")}>
      {t("core.accounts.signup.signin")}
    </Link>
  );

  if (!session.signupOpen && checkFailed) {
    // We could not tell whether sign-up is open: say so rather than claim "by invitation".
    const unknown = new ApiError({
      status: 0,
      code: "unavailable",
      method: "GET",
      route: "/api/accounts/session",
      module: "accounts",
      traceId: "",
    });
    return (
      <div className="mx-auto flex max-w-md flex-col gap-4">
        <h1 className="text-2xl font-bold">{t("core.accounts.signup.title")}</h1>
        <ErrorState error={unknown} onRetry={() => void refreshSession()} />
        {signInLink}
      </div>
    );
  }

  if (!session.signupOpen) {
    return (
      <div className="mx-auto flex max-w-md flex-col gap-4">
        <h1 className="text-2xl font-bold">{t("core.accounts.signup.title")}</h1>
        <EmptyState action={signInLink}>{t("core.accounts.signup.invitation")}</EmptyState>
      </div>
    );
  }

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const data = new FormData(e.currentTarget);
    void action.run(async () => {
      await api.POST("/api/accounts/sign-up", {
        body: { email: text(data.get("email")), password: text(data.get("password")) },
      });
      await refreshSession();
      await navigate(returnTo, { replace: true });
    });
  };

  const message =
    error?.code === "unsupported"
      ? t("core.accounts.signup.invitation")
      : error?.code === "conflict"
        ? t("core.accounts.signup.taken")
        : undefined;

  return (
    <div className="mx-auto flex max-w-md flex-col gap-4">
      <h1 className="text-2xl font-bold">{t("core.accounts.signup.title")}</h1>
      <form ref={formRef} onSubmit={submit} className="flex flex-col gap-4" noValidate>
        <ErrorSummary
          error={error}
          formRef={formRef}
          {...(message !== undefined ? { message } : {})}
        />
        <Field
          label={t("core.accounts.signup.email")}
          name="email"
          type="email"
          inputMode="email"
          autoComplete="username"
          required
          error={fieldErrors["email"]}
        />
        <PasswordField
          label={t("core.accounts.signup.password")}
          name="password"
          autoComplete="new-password"
          hint={t("core.accounts.signup.hint", { min: DEFAULT_MIN_PASSWORD })}
          error={fieldErrors["password"]}
        />
        <ReadOnlyNote id="signup-readonly" readOnly={readOnly} />
        <Button
          type="submit"
          busy={action.busy}
          disabled={readOnly}
          {...(readOnly ? { "aria-describedby": "signup-readonly" } : {})}
        >
          {t("core.accounts.signup.submit")}
        </Button>
      </form>
      {signInLink}
    </div>
  );
}
