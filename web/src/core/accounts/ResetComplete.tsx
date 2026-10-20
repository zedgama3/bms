import { useMemo, useRef, useState } from "react";
import type { FormEvent } from "react";
import { Link, useNavigate } from "react-router";
import { api, ApiError, refreshSession, useReadOnly } from "@/api";
import { useT } from "@/i18n";
import { Button, ErrorState, ErrorSummary, Field, linkButtonClass, useFieldErrors } from "@/ui";
import type { FieldMessage } from "@/ui";
import { DEFAULT_MIN_PASSWORD } from "./constants";
import { PasswordField } from "./PasswordField";
import { ReadOnlyNote } from "./ReadOnlyNote";
import { text } from "./text";
import { useAction } from "./useAction";

/** A link that is no use: the same state whether the token is missing, unknown, used or
 *  replaced (`invalid`), or too old (`expired`). It has no inputs to fix, only a way to ask again. */
const NO_TOKEN = new ApiError({
  status: 400,
  code: "invalid",
  method: "POST",
  route: "/api/accounts/password-reset/complete",
  module: "accounts",
  traceId: "",
});

/** The new-password form behind the e-mailed link. The token arrives as a prop (the caller read
 *  it from the address and removed it there), is sent only in the body of the complete request,
 *  and is never stored. On success the person goes to sign-in: nothing signs them in. */
export function ResetComplete({ token }: { token: string }) {
  const t = useT();
  const navigate = useNavigate();
  const { readOnly } = useReadOnly();
  const formRef = useRef<HTMLFormElement>(null);
  const action = useAction();
  const fieldErrors = useFieldErrors(action.error);
  const [mismatch, setMismatch] = useState<FieldMessage | undefined>(undefined);

  const tokenCode = useMemo(() => {
    if (token === "") return "invalid";
    const f = action.error?.fields.find((x) => x.field === "token");
    if (!f) return null;
    return f.code === "expired" ? "expired" : "invalid";
  }, [token, action.error]);

  if (tokenCode !== null) {
    return (
      <div className="mx-auto flex max-w-md flex-col gap-4">
        <h1 className="text-2xl font-bold">{t("core.accounts.reset.complete.title")}</h1>
        <ErrorState
          error={action.error ?? NO_TOKEN}
          message={
            tokenCode === "expired"
              ? t("core.accounts.reset.complete.expired")
              : t("core.accounts.reset.complete.invalid")
          }
        />
        <div>
          <Link to="/accounts/password-reset" className={linkButtonClass("primary")}>
            {t("core.accounts.reset.complete.again")}
          </Link>
        </div>
      </div>
    );
  }

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = e.currentTarget;
    const data = new FormData(form);
    const password = text(data.get("new_password"));
    if (password !== text(data.get("confirm_password"))) {
      setMismatch({ text: t("core.accounts.reset.complete.mismatch") });
      form.querySelector<HTMLElement>('[name="confirm_password"]')?.focus();
      return;
    }
    setMismatch(undefined);
    void action.run(async () => {
      await api.POST("/api/accounts/password-reset/complete", {
        body: { token, new_password: password },
      });
      // Every session of the account has ended on the server; let the page know before the
      // sign-in screen decides whether to send a signed-in person away.
      await refreshSession();
      await navigate("/accounts/sign-in", { replace: true, state: { passwordReset: true } });
    });
  };

  return (
    <div className="mx-auto flex max-w-md flex-col gap-4">
      <h1 className="text-2xl font-bold">{t("core.accounts.reset.complete.title")}</h1>
      <form ref={formRef} onSubmit={submit} className="flex flex-col gap-4" noValidate>
        <ErrorSummary error={action.error} formRef={formRef} />
        <PasswordField
          label={t("core.accounts.reset.complete.new")}
          name="new_password"
          kind="new"
          autoComplete="new-password"
          hint={t("core.accounts.password.new_hint", { min: DEFAULT_MIN_PASSWORD })}
          error={fieldErrors["new_password"]}
        />
        <Field
          label={t("core.accounts.reset.complete.confirm")}
          name="confirm_password"
          type="password"
          autoComplete="new-password"
          required
          error={mismatch}
        />
        <ReadOnlyNote id="reset-complete-readonly" readOnly={readOnly} />
        <Button
          type="submit"
          busy={action.busy}
          disabled={readOnly}
          {...(readOnly ? { "aria-describedby": "reset-complete-readonly" } : {})}
        >
          {t("core.accounts.reset.complete.submit")}
        </Button>
      </form>
    </div>
  );
}
