import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Check } from "lucide-react";
import { Link } from "react-router";
import { ApiError, useReadOnly } from "@/api";
import { isMessageKey, useT } from "@/i18n";
import { Button, useToast } from "@/ui";
import { vol } from "./calls";
import { focusSoon } from "./helpers";
import { ReadOnlyNote } from "./ReadOnlyNote";
import { ScreenError } from "./ScreenError";
import type { Qualification, ShiftSummary } from "./types";

/** Why a person cannot sign up, from the server's `eligibility` (missing qualifications named). */
export function useWhyNot(qualifications: readonly Qualification[] | undefined) {
  const t = useT();
  return (s: ShiftSummary): string => {
    const e = s.eligibility ?? {};
    const parts: string[] = [];
    if (s.canceled_at) return t("module.volunteers.why.canceled");
    if (e.reason) {
      const key = `module.volunteers.why.${e.reason}`;
      parts.push(isMessageKey(key) ? t(key) : t("module.volunteers.why.unknown"));
    }
    const missing = e.missing ?? [];
    if (missing.length > 0) {
      const names = missing.map(
        (id) =>
          qualifications?.find((q) => q.id === id)?.name ??
          t("module.volunteers.why.a_qualification"),
      );
      parts.push(
        t("module.volunteers.why.missing", { names: names.join(", "), count: names.length }),
      );
    }
    return parts.length > 0 ? parts.join(" ") : t("module.volunteers.why.unknown");
  };
}

/** The short reason a list row shows instead of the sentence (V4.3): `why_short.*`, with the
 *  missing qualifications named. */
export function useWhyShort(qualifications: readonly Qualification[] | undefined) {
  const t = useT();
  return (s: ShiftSummary): string => {
    const e = s.eligibility ?? {};
    const missing = e.missing ?? [];
    if (missing.length > 0) {
      const names = missing.map(
        (id) =>
          qualifications?.find((q) => q.id === id)?.name ??
          t("module.volunteers.why.a_qualification"),
      );
      return t("module.volunteers.why_short.missing", { names: names.join(", ") });
    }
    const key = `module.volunteers.why_short.${e.reason ?? ""}`;
    return isMessageKey(key) && e.reason ? t(key) : t("module.volunteers.why_short.unknown");
  };
}

/** The sign-up part of a shift card or page: **Sign up** when the server says the person is
 *  eligible, **Booked** when they hold a booking, else the reason. A refused sign-up shows its
 *  reason in place (the shell's forbidden sentence for a 403). */
export function SignUpControl({
  shift,
  qualifications,
  short = false,
}: {
  shift: ShiftSummary;
  qualifications: readonly Qualification[] | undefined;
  /** A list row: a reason is the short phrase, not the sentence. */
  short?: boolean;
}) {
  const t = useT();
  const toast = useToast();
  const client = useQueryClient();
  const { readOnly } = useReadOnly();
  const whyNot = useWhyNot(qualifications);
  const whyShort = useWhyShort(qualifications);
  const statusId = `signup-status-${shift.id}`;
  const signUp = useMutation({
    mutationFn: () => vol.signUp(shift.id),
    onSuccess: async () => {
      toast.show(t("module.volunteers.signed_up"));
      await client.invalidateQueries({ queryKey: ["volunteers"] });
      // The button is gone: the new status takes focus.
      focusSoon(`#${statusId}`);
    },
    // A refusal means the card was stale (full, already booked): refresh it.
    onError: async (e) => {
      if (e instanceof ApiError && e.code === "conflict")
        await client.invalidateQueries({ queryKey: ["volunteers"] });
    },
  });
  const error = signUp.error instanceof ApiError ? signUp.error : null;
  const noteId = `signup-readonly-${shift.id}`;
  // A canceled shift says so itself (its banner, its card); nothing to sign up for.
  if (shift.canceled_at) return null;

  return (
    <div className="flex flex-col gap-2">
      {shift.mine ? (
        <p id={statusId} tabIndex={-1} className="flex items-center gap-2 font-semibold">
          <Check aria-hidden="true" className="size-5 shrink-0" />
          {t("module.volunteers.booked")}
        </p>
      ) : shift.eligibility?.eligible === true ? (
        <div className="flex flex-col gap-1">
          <div>
            <Button
              busy={signUp.isPending}
              disabled={readOnly}
              {...(readOnly ? { "aria-describedby": noteId } : {})}
              onClick={() => signUp.mutate()}
            >
              {t("module.volunteers.sign_up")}
              <span className="sr-only"> {shift.label}</span>
            </Button>
          </div>
          <ReadOnlyNote id={noteId} readOnly={readOnly} />
        </div>
      ) : (
        <p className="text-muted">{short ? whyShort(shift) : whyNot(shift)}</p>
      )}
      {error && !shift.mine ? (
        error.code === "conflict" && error.fields.some((f) => f.code === "unnamed") ? (
          // The person has no playa name yet (decision 26): the fix is on their profile.
          <p role="alert" className="font-semibold text-danger">
            {t("module.volunteers.signup.unnamed")}{" "}
            <Link to="/people/me" className="inline-flex min-h-11 items-center underline">
              {t("module.volunteers.signup.unnamed_link")}
            </Link>
          </p>
        ) : (
          <ScreenError error={error} />
        )
      ) : null}
    </div>
  );
}
