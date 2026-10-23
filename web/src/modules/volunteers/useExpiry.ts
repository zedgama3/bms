import { useState } from "react";
import type { ChangeEvent } from "react";
import { useT } from "@/i18n";
import { addDays } from "./helpers";
import type { Qualification } from "./types";

/** The qualification, acquired and expires fields of a record form. Left empty, the end date
 *  is the acquired date plus the qualification's validity (or none when it has none), so the
 *  field is filled in with that date until the person types their own, and its hint says so. */
export function useExpiry(catalogue: readonly Qualification[] | undefined) {
  const t = useT();
  const [qualification, setQualification] = useState("");
  const [acquired, setAcquired] = useState("");
  const [expires, setExpires] = useState("");
  const [typed, setTyped] = useState(false);
  const [never, setNever] = useState(false);

  const days = catalogue?.find((q) => q.id === qualification)?.validity_days ?? 0;
  const fill = (q: string, a: string) => {
    const d = catalogue?.find((x) => x.id === q)?.validity_days ?? 0;
    return d > 0 && a !== "" ? addDays(a, d) : "";
  };
  return {
    qualification: {
      value: qualification,
      onChange: (e: ChangeEvent<HTMLSelectElement>) => {
        setQualification(e.target.value);
        if (!typed) setExpires(fill(e.target.value, acquired));
      },
    },
    acquired: {
      value: acquired,
      onChange: (e: ChangeEvent<HTMLInputElement>) => {
        setAcquired(e.target.value);
        if (!typed) setExpires(fill(qualification, e.target.value));
      },
    },
    expires: {
      value: expires,
      onChange: (e: ChangeEvent<HTMLInputElement>) => {
        setExpires(e.target.value);
        setTyped(true);
      },
    },
    /** The **Never expires** switch: checked clears and disables the end date; the form then
     *  sends `no_expiry: true` instead of a date. */
    never: {
      checked: never,
      onChange: (e: ChangeEvent<HTMLInputElement>) => {
        setNever(e.target.checked);
        if (e.target.checked) setExpires("");
        else if (!typed) setExpires(fill(qualification, acquired));
      },
    },
    hint: never
      ? t("module.volunteers.label.expires_on_hint_never")
      : days > 0
        ? t("module.volunteers.label.expires_on_hint_days", { days })
        : t("module.volunteers.label.expires_on_hint"),
    reset: () => {
      setQualification("");
      setAcquired("");
      setExpires("");
      setTyped(false);
      setNever(false);
    },
  };
}
