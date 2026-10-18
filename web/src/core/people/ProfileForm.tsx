import { useEffect, useRef } from "react";
import type { FormEvent } from "react";
import { useReadOnly } from "@/api";
import { isMessageKey, useLanguage, useT } from "@/i18n";
import { Button, controlClass, ErrorSummary, Field, useFieldErrors, useToast } from "@/ui";
import { ReadOnlyNote } from "./ReadOnlyNote";
import { profileApi } from "./profileApi";
import type { Target } from "./profileApi";
import { text } from "./text";
import type { Profile } from "./types";
import { useAction } from "./useAction";

/** The "About you" fields: playa name, pronouns, language (own profile only), legal name and
 *  phone unless the regional hides them, a contact e-mail for a person with no account. The
 *  regional's modes mark a field required on the person's own save; an admin's save is not
 *  held to them. Input `name`s equal the API's field paths so errors land on them. */
export function ProfileForm({
  profile,
  target,
  onSaved,
  focusPlayaName = false,
}: {
  profile: Profile;
  target: Target;
  onSaved: (saved: Profile) => void;
  /** The first-sign-in prompt: put the cursor in the playa name. */
  focusPlayaName?: boolean;
}) {
  const t = useT();
  const toast = useToast();
  const { readOnly } = useReadOnly();
  const { current: currentLanguage, offered, set: setLanguage } = useLanguage();
  const formRef = useRef<HTMLFormElement>(null);
  const act = useAction();
  const fieldErrors = useFieldErrors(act.error);
  const own = target === "me";
  const { person, policy } = profile;
  const has = (group: string) => profile.fields.includes(group);

  useEffect(() => {
    if (!focusPlayaName) return;
    // After the route announcer has moved focus to the heading, so the cursor ends in the field.
    const id = setTimeout(() => {
      const el = formRef.current?.elements.namedItem("playa_name");
      if (el instanceof HTMLElement) el.focus();
    }, 0);
    return () => clearTimeout(id);
  }, [focusPlayaName]);

  const showLegal = policy.legal_name !== "hidden" && (own || has("legal_name"));
  const showPhone = policy.phone !== "hidden" && (own || has("phone"));
  const showContactEmail = !person.has_account && (own || has("contact_email"));
  const showLanguage = own && offered.length > 1;
  const showLogin = person.has_account && person.email && (own || has("email"));
  const mustLegal = own && policy.legal_name === "required";
  const mustPhone = own && policy.phone === "required";

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const data = new FormData(e.currentTarget);
    const v = (name: string) => text(data.get(name)).trim();
    const language = v("language");
    void act.run(async () => {
      const saved = await profileApi.update(target, {
        playa_name: v("playa_name"),
        pronouns: v("pronouns"),
        ...(showLanguage ? { language } : {}),
        ...(showLegal ? { legal_name: v("legal_name") } : {}),
        ...(showPhone ? { phone: v("phone") } : {}),
        ...(showContactEmail ? { contact_email: v("contact_email") } : {}),
      });
      if (showLanguage && language) setLanguage(language);
      toast.show(t("core.people.profile.saved"));
      onSaved(saved);
    });
  };

  return (
    <form ref={formRef} onSubmit={submit} className="flex flex-col gap-4" noValidate>
      <ErrorSummary error={act.error} formRef={formRef} />
      <fieldset disabled={readOnly} className="m-0 flex min-w-0 flex-col gap-4 border-0 p-0">
        <Field
          label={t("core.people.profile.playa_name")}
          name="playa_name"
          hint={t("core.people.profile.playa_name_hint")}
          required={own}
          error={fieldErrors["playa_name"]}
        >
          {(control) => (
            <input
              {...control}
              type="text"
              maxLength={100}
              autoComplete="off"
              defaultValue={person.playa_name}
              className={controlClass}
              {...(focusPlayaName
                ? {
                    "aria-describedby": [control["aria-describedby"], "first-prompt"]
                      .filter(Boolean)
                      .join(" "),
                  }
                : {})}
            />
          )}
        </Field>
        <Field
          label={t("core.people.profile.pronouns")}
          name="pronouns"
          maxLength={40}
          autoComplete="off"
          defaultValue={person.pronouns}
          error={fieldErrors["pronouns"]}
        />
        {showLanguage ? (
          <Field
            label={t("core.people.profile.language")}
            name="language"
            error={fieldErrors["language"]}
          >
            {(control) => (
              <select
                {...control}
                className={controlClass}
                defaultValue={person.language || currentLanguage}
              >
                {offered.map((tag) => {
                  const key = `shell.language.${tag}`;
                  return (
                    <option key={tag} value={tag} lang={tag}>
                      {isMessageKey(key) ? t(key) : tag}
                    </option>
                  );
                })}
              </select>
            )}
          </Field>
        ) : null}
        {showLegal ? (
          <Field
            label={t("core.people.profile.legal_name")}
            name="legal_name"
            hint={t("core.people.profile.legal_name_hint")}
            required={mustLegal}
            maxLength={200}
            autoComplete={own ? "name" : "off"}
            defaultValue={person.legal_name ?? ""}
            error={fieldErrors["legal_name"]}
          />
        ) : null}
        {showPhone ? (
          <Field
            label={t("core.people.profile.phone")}
            name="phone"
            type="tel"
            inputMode="tel"
            required={mustPhone}
            maxLength={40}
            autoComplete={own ? "tel" : "off"}
            defaultValue={person.phone ?? ""}
            error={fieldErrors["phone"]}
          />
        ) : null}
        {showContactEmail ? (
          <Field
            label={t("core.people.profile.contact_email")}
            name="contact_email"
            type="email"
            inputMode="email"
            autoComplete="off"
            defaultValue={person.contact_email ?? ""}
            error={fieldErrors["contact_email"]}
          />
        ) : null}
        {showLogin ? (
          <div className="flex flex-col gap-1">
            <p className="font-semibold">{t("core.people.profile.login_email")}</p>
            <p className="break-all">{person.email}</p>
            {own ? (
              <p className="text-sm text-muted">{t("core.people.profile.login_email_hint")}</p>
            ) : null}
          </div>
        ) : null}
      </fieldset>
      <ReadOnlyNote id="profile-readonly" readOnly={readOnly} />
      <div>
        <Button
          type="submit"
          busy={act.busy}
          disabled={readOnly}
          {...(readOnly ? { "aria-describedby": "profile-readonly" } : {})}
        >
          {t("core.people.profile.save")}
        </Button>
      </div>
    </form>
  );
}
