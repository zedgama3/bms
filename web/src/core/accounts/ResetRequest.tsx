import { useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { Link } from "react-router";
import { api, useAction, useReadOnly } from "@/api";
import { useT } from "@/i18n";
import { Button, ErrorSummary, Field, linkButtonClass, useFieldErrors } from "@/ui";
import { ReadOnlyNote } from "./ReadOnlyNote";
import { text } from "./text";

/** The "send me a link" form. Every 202 shows the same sentence whatever the address, and a 501
 *  the same sentence for every address (identity.md "Uniform answers"): nothing here depends on
 *  whether an account exists. */
export function ResetRequest() {
  const t = useT();
  const { readOnly } = useReadOnly();
  const formRef = useRef<HTMLFormElement>(null);
  const action = useAction();
  const fieldErrors = useFieldErrors(action.error);
  const [sent, setSent] = useState(false);
  const panel = useRef<HTMLDivElement>(null);
  // The form is gone once the answer shows; keep the keyboard and screen-reader position.
  useEffect(() => {
    if (sent) panel.current?.focus();
  }, [sent]);

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const data = new FormData(e.currentTarget);
    void action.run(async () => {
      await api.POST("/api/accounts/password-reset", { body: { email: text(data.get("email")) } });
      setSent(true);
    });
  };

  const back = (
    <Link to="/accounts/sign-in" className={linkButtonClass("secondary")}>
      {t("core.accounts.reset.back")}
    </Link>
  );

  if (sent) {
    return (
      <div className="mx-auto flex max-w-md flex-col gap-4">
        <h1 className="text-2xl font-bold">{t("core.accounts.reset.title")}</h1>
        <div
          ref={panel}
          role="status"
          tabIndex={-1}
          className="flex flex-col gap-2 rounded-md border border-border p-4"
        >
          <p className="font-semibold">{t("core.accounts.reset.request.sent")}</p>
          <p>{t("core.accounts.reset.request.sent_hint")}</p>
        </div>
        <div>{back}</div>
      </div>
    );
  }

  return (
    <div className="mx-auto flex max-w-md flex-col gap-4">
      <h1 className="text-2xl font-bold">{t("core.accounts.reset.title")}</h1>
      <p>{t("core.accounts.reset.request.intro")}</p>
      <form ref={formRef} onSubmit={submit} className="flex flex-col gap-4" noValidate>
        <ErrorSummary
          error={action.error}
          formRef={formRef}
          {...(action.error?.code === "unsupported"
            ? { message: t("core.accounts.reset.request.not_configured") }
            : {})}
        />
        <Field
          label={t("core.accounts.reset.request.email")}
          name="email"
          type="email"
          inputMode="email"
          autoComplete="username"
          required
          error={fieldErrors["email"]}
        />
        <ReadOnlyNote id="reset-request-readonly" readOnly={readOnly} />
        <Button
          type="submit"
          busy={action.busy}
          disabled={readOnly}
          {...(readOnly ? { "aria-describedby": "reset-request-readonly" } : {})}
        >
          {t("core.accounts.reset.request.submit")}
        </Button>
      </form>
      <div>{back}</div>
    </div>
  );
}
