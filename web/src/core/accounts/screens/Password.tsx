import { useRef, useState } from "react";
import type { FormEvent } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { api, refreshSession, safeReturnTo, useAction, useReadOnly, useSession } from "@/api";
import { useT } from "@/i18n";
import { Banner, Button, Dialog, ErrorSummary, useFieldErrors, useToast } from "@/ui";
import { DEFAULT_MIN_PASSWORD } from "../constants";
import { PasswordField } from "../PasswordField";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { text } from "../text";

/** `/accounts/password`: change the password, and sign out everywhere else. Works for a
 *  restricted session (a temporary password), where it is the only screen that does. */
export function Password() {
  const t = useT();
  const session = useSession();
  const { readOnly } = useReadOnly();
  const toast = useToast();
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const formRef = useRef<HTMLFormElement>(null);
  const change = useAction();
  const fieldErrors = useFieldErrors(change.error);
  const others = useAction();
  const [confirming, setConfirming] = useState(false);
  const othersForm = useRef<HTMLFormElement>(null);
  const restricted = session.signedIn && session.passwordChangeRequired;

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = e.currentTarget;
    const data = new FormData(form);
    void change.run(async () => {
      await api.POST("/api/accounts/password", {
        body: {
          current_password: text(data.get("current_password")),
          new_password: text(data.get("new_password")),
        },
      });
      form.reset();
      await refreshSession();
      toast.show(t("core.accounts.password.changed"));
      if (restricted) await navigate(safeReturnTo(params.get("returnTo")), { replace: true });
    });
  };

  const endOthers = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    void others.run(async () => {
      await api.POST("/api/accounts/sessions/end-others");
      await refreshSession();
      setConfirming(false);
      toast.show(t("core.accounts.others.done"));
    });
  };

  return (
    <div className="mx-auto flex max-w-md flex-col gap-8">
      <section aria-labelledby="password-title" className="flex flex-col gap-4">
        <h1 id="password-title" className="text-2xl font-bold">
          {t("core.accounts.password.title")}
        </h1>
        {restricted ? <Banner live={false}>{t("core.accounts.password.required")}</Banner> : null}
        <h2 id="password-change-title" className="text-xl font-bold">
          {t("core.accounts.password.section")}
        </h2>
        <form
          ref={formRef}
          onSubmit={submit}
          aria-labelledby="password-change-title"
          className="flex flex-col gap-4"
          noValidate
        >
          <ErrorSummary error={change.error} formRef={formRef} />
          <PasswordField
            label={t("core.accounts.password.current")}
            name="current_password"
            kind="current"
            autoComplete="current-password"
            error={fieldErrors["current_password"]}
          />
          <PasswordField
            label={t("core.accounts.password.new")}
            name="new_password"
            kind="new"
            autoComplete="new-password"
            hint={t("core.accounts.password.new_hint", { min: DEFAULT_MIN_PASSWORD })}
            error={fieldErrors["new_password"]}
          />
          <ReadOnlyNote id="password-readonly" readOnly={readOnly} />
          <Button
            type="submit"
            busy={change.busy}
            disabled={readOnly}
            {...(readOnly ? { "aria-describedby": "password-readonly" } : {})}
          >
            {t("core.accounts.password.submit")}
          </Button>
        </form>
      </section>

      {restricted ? null : (
        <section aria-labelledby="others-title" className="flex flex-col gap-3">
          <h2 id="others-title" className="text-xl font-bold">
            {t("core.accounts.others.title")}
          </h2>
          <p>{t("core.accounts.others.body")}</p>
          <ReadOnlyNote id="others-readonly" readOnly={readOnly} />
          <div>
            <Button
              variant="secondary"
              disabled={readOnly}
              {...(readOnly ? { "aria-describedby": "others-readonly" } : {})}
              onClick={() => {
                others.clear();
                setConfirming(true);
              }}
            >
              {t("core.accounts.others.button")}
            </Button>
          </div>
        </section>
      )}

      <Dialog
        open={confirming}
        title={t("core.accounts.others.confirm_title")}
        onClose={() => setConfirming(false)}
      >
        <form ref={othersForm} onSubmit={endOthers} className="flex flex-col gap-4">
          <ErrorSummary error={others.error} formRef={othersForm} />
          <p>{t("core.accounts.others.confirm_body")}</p>
          <div className="flex flex-wrap gap-3">
            <Button type="submit" busy={others.busy}>
              {t("core.accounts.others.confirm")}
            </Button>
            <Button variant="secondary" onClick={() => setConfirming(false)}>
              {t("core.accounts.cancel")}
            </Button>
          </div>
        </form>
      </Dialog>
    </div>
  );
}
