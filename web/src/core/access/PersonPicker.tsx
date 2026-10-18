import { useEffect, useRef, useState } from "react";
import type { KeyboardEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, ApiError } from "@/api";
import { useT } from "@/i18n";
import { Button, ErrorState, Field, Loading } from "@/ui";
import type { FieldMessage } from "@/ui";
import type { Match } from "./types";

const MIN = 2;

/** Pick a person by searching their playa name (`GET /api/people/search`). The typed term
 *  stays in this component: never in the URL or storage (it is personal). A chosen person is
 *  shown by name with a "Change" button; the parent holds only their ID. */
export function PersonPicker({
  name,
  label,
  chosenName,
  hasValue,
  onPick,
  onClear,
  error,
  disabled = false,
}: {
  /** The input's `name`, equal to the API's field path, so a field error lands here. */
  name: string;
  label: string;
  /** The chosen person's display name (or ID until names arrive). */
  chosenName: string;
  hasValue: boolean;
  onPick: (match: Match) => void;
  onClear: () => void;
  error?: FieldMessage | undefined;
  disabled?: boolean;
}) {
  const t = useT();
  const [term, setTerm] = useState("");
  const [submitted, setSubmitted] = useState("");
  const changeRef = useRef<HTMLButtonElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const seen = useRef(hasValue);
  // After choosing, the search box is gone: focus lands on "Choose someone else"; after
  // clearing, it returns to the search box.
  useEffect(() => {
    if (seen.current === hasValue) return;
    seen.current = hasValue;
    if (hasValue) changeRef.current?.focus();
    else searchRef.current?.focus();
  }, [hasValue]);

  const q = useQuery({
    queryKey: ["access", "people-search", submitted],
    enabled: submitted.length >= MIN,
    queryFn: ({ signal }) =>
      api.GET("/api/people/search", { signal, params: { query: { q: submitted } } }),
  });

  if (hasValue) {
    return (
      <div className="flex flex-col gap-1">
        <span className="font-semibold">{label}</span>
        <div className="flex flex-wrap items-center gap-3">
          <span className="min-w-0 break-all text-lg">{chosenName}</span>
          <Button ref={changeRef} variant="secondary" disabled={disabled} onClick={onClear}>
            {t("core.access.picker.change")}
          </Button>
        </div>
      </div>
    );
  }

  const search = () => {
    const v = term.trim();
    if (v.length >= MIN) setSubmitted(v);
  };
  const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") {
      e.preventDefault();
      search();
    }
  };
  const error_ = q.error instanceof ApiError ? q.error : null;
  const matches = q.data?.matches ?? [];

  return (
    <div className="flex flex-col gap-3" role="search">
      <Field
        label={label}
        name={name}
        hint={t("core.access.picker.hint", { min: MIN })}
        error={error}
      >
        {(control) => (
          <input
            {...control}
            ref={searchRef}
            type="search"
            autoComplete="off"
            enterKeyHint="search"
            maxLength={100}
            value={term}
            disabled={disabled}
            onKeyDown={onKey}
            onChange={(e) => setTerm(e.target.value)}
            className="block min-h-11 w-full rounded-md border border-border bg-bg px-3 py-2 text-base text-fg aria-[invalid=true]:border-danger"
          />
        )}
      </Field>
      <div>
        <Button
          variant="secondary"
          disabled={disabled || term.trim().length < MIN}
          onClick={search}
        >
          {t("core.access.picker.search")}
        </Button>
      </div>
      {q.isFetching ? <Loading /> : null}
      {error_ ? (
        <ErrorState
          error={error_}
          {...(error_.code === "permission"
            ? { message: t("core.access.picker.forbidden") }
            : { onRetry: () => void q.refetch() })}
        />
      ) : null}
      <p role="status" className="text-sm text-muted">
        {q.data ? t("core.access.picker.found", { count: matches.length }) : ""}
      </p>
      {matches.length > 0 ? (
        <ul className="flex flex-col gap-2" aria-label={t("core.access.picker.results")}>
          {matches.map((m) => (
            <li key={m.id}>
              <button
                type="button"
                onClick={() => onPick(m)}
                className="flex min-h-11 w-full flex-wrap items-center gap-x-2 rounded-md border border-border bg-surface px-3 py-2 text-start"
              >
                <span className="font-semibold">{m.playa_name || m.id}</span>
                {m.pronouns ? <span className="text-muted">({m.pronouns})</span> : null}
              </button>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}
