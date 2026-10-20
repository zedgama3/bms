import { useId } from "react";
import { useT } from "@/i18n";
import { Button, controlClass, FieldMessageText } from "@/ui";
import type { FieldMessage } from "@/ui";
import { byName, focusSoon } from "./focus";

/** A secret setting: whether a value is set (never the value), Replace (a password field for
 *  the new value) and Clear. `draft` is what the person has asked for: nothing, a new value
 *  (possibly still empty), or null for cleared. */
export function SecretControl({
  name,
  label,
  hint,
  isSet,
  draft,
  onDraft,
  error,
}: {
  name: string;
  label: string;
  hint: string;
  isSet: boolean;
  draft: string | null | undefined;
  onDraft: (v: string | null | undefined) => void;
  error: FieldMessage | undefined;
}) {
  const t = useT();
  const id = useId();
  const errId = `${id}-err`;
  const helpId = `${id}-help`;
  const replacing = draft !== undefined && draft !== null;
  const clearing = draft === null;
  const status = clearing
    ? t("core.settings.secret.clear_pending")
    : isSet
      ? t("core.settings.secret.set")
      : t("core.settings.secret.unset");
  return (
    <div className="flex flex-col gap-2">
      <p className="font-semibold">{label}</p>
      {hint ? <p className="text-sm text-muted">{hint}</p> : null}
      <p>{status}</p>
      {replacing ? (
        <div className="flex flex-col gap-1">
          <label htmlFor={id} className="font-semibold">
            {t("core.settings.secret.new", { setting: label })}
          </label>
          <p id={helpId} className="text-sm text-muted">
            {t("core.settings.secret.field_help")}
          </p>
          <input
            id={id}
            name={name}
            type="password"
            autoComplete="new-password"
            autoCapitalize="off"
            spellCheck={false}
            className={controlClass}
            value={draft}
            aria-describedby={error ? `${helpId} ${errId}` : helpId}
            aria-invalid={error ? true : undefined}
            onChange={(e) => onDraft(e.target.value)}
          />
        </div>
      ) : null}
      {error ? (
        <p id={errId} className="text-sm font-semibold text-danger">
          <span aria-hidden="true">! </span>
          <FieldMessageText message={error} />
        </p>
      ) : null}
      <div className="flex flex-wrap gap-2">
        {replacing ? (
          <Button variant="secondary" onClick={() => onDraft(undefined)}>
            {t("core.settings.secret.cancel")}
          </Button>
        ) : clearing ? (
          <Button
            variant="secondary"
            aria-label={t("core.settings.secret.keep_label", { setting: label })}
            onClick={() => onDraft(undefined)}
          >
            {t("core.settings.secret.keep")}
          </Button>
        ) : (
          <>
            <Button
              variant="secondary"
              aria-label={t("core.settings.secret.replace_label", { setting: label })}
              onClick={() => {
                onDraft("");
                focusSoon(byName(name));
              }}
            >
              {t("core.settings.secret.replace")}
            </Button>
            {isSet ? (
              <Button
                variant="secondary"
                aria-label={t("core.settings.secret.clear_label", { setting: label })}
                onClick={() => onDraft(null)}
              >
                {t("core.settings.secret.clear")}
              </Button>
            ) : null}
          </>
        )}
      </div>
    </div>
  );
}
