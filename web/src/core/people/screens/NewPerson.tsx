import { useRef } from "react";
import type { FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router";
import { api, useMe, useReadOnly } from "@/api";
import { useT } from "@/i18n";
import { Button, ErrorSummary, Field, useFieldErrors, useToast } from "@/ui";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { text } from "../text";
import { useAction } from "../useAction";

/** `/people/new`: a profile for someone with no account (a walk-on, a paper form). Only the
 *  playa name is required; a lead may not know the rest. */
export function NewPerson() {
  const t = useT();
  const toast = useToast();
  const navigate = useNavigate();
  const client = useQueryClient();
  const policy = useMe().data?.policy;
  const showLegal = policy?.legal_name !== "hidden";
  const showPhone = policy?.phone !== "hidden";
  const { readOnly } = useReadOnly();
  const formRef = useRef<HTMLFormElement>(null);
  const act = useAction();
  const fieldErrors = useFieldErrors(act.error);

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const data = new FormData(e.currentTarget);
    const v = (name: string) => text(data.get(name)).trim();
    void act.run(async () => {
      const created = await api.POST("/api/people/", {
        body: {
          playa_name: v("playa_name"),
          pronouns: v("pronouns"),
          ...(showLegal ? { legal_name: v("legal_name") } : {}),
          ...(showPhone ? { phone: v("phone") } : {}),
          contact_email: v("contact_email"),
        },
      });
      void client.invalidateQueries({ queryKey: ["people"] });
      toast.show(t("core.people.new.done"));
      void navigate(`/people/${created.person.id}`, { replace: true });
    });
  };

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-2xl font-bold">{t("core.people.new.title")}</h1>
      <div>
        <Link to="/people" className="inline-flex min-h-11 items-center underline">
          {t("core.people.new.back")}
        </Link>
      </div>
      <form ref={formRef} onSubmit={submit} className="flex flex-col gap-4" noValidate>
        <ErrorSummary error={act.error} formRef={formRef} />
        <Field
          label={t("core.people.profile.playa_name")}
          name="playa_name"
          required
          maxLength={100}
          autoComplete="off"
          error={fieldErrors["playa_name"]}
        />
        <Field
          label={t("core.people.profile.pronouns")}
          name="pronouns"
          maxLength={40}
          autoComplete="off"
          error={fieldErrors["pronouns"]}
        />
        {showLegal ? (
          <Field
            label={t("core.people.profile.legal_name")}
            name="legal_name"
            maxLength={200}
            autoComplete="off"
            error={fieldErrors["legal_name"]}
          />
        ) : null}
        {showPhone ? (
          <Field
            label={t("core.people.profile.phone")}
            name="phone"
            type="tel"
            inputMode="tel"
            maxLength={40}
            autoComplete="off"
            error={fieldErrors["phone"]}
          />
        ) : null}
        <Field
          label={t("core.people.profile.contact_email")}
          name="contact_email"
          type="email"
          inputMode="email"
          autoComplete="off"
          error={fieldErrors["contact_email"]}
        />
        <ReadOnlyNote id="new-readonly" readOnly={readOnly} />
        <div>
          <Button
            type="submit"
            busy={act.busy}
            disabled={readOnly}
            {...(readOnly ? { "aria-describedby": "new-readonly" } : {})}
          >
            {t("core.people.new.submit")}
          </Button>
        </div>
      </form>
    </div>
  );
}
