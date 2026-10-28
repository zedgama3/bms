import { useEffect, useRef, useState } from "react";
import type { KeyboardEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import type { ApiError, components } from "@/api";
import { useT } from "@/i18n";
import { Button } from "./Button";
import { ErrorState } from "./errors";
import type { FieldMessage } from "./errors";
import { Field } from "./Field";
import { Loading } from "./Loading";

export type PersonMatch = components["schemas"]["PeopleMatchList"]["matches"][number];

const MIN = 2;

// ui imports only types from @/api, so the class is recognised by its name (as Attachments does).
const isApiError = (e: unknown): e is ApiError =>
  typeof e === "object" && e !== null && (e as { name?: unknown }).name === "ApiError";

/** Pick a person by searching their playa name. `search` is the screen's call through `@/api`
 *  (normally `GET /api/people/search`; the kit makes no request of its own). The typed term
 *  stays in this component: never in the URL or storage (it is personal). A chosen person is
 *  shown by name with a "Change" button; the parent holds only their ID. */
export function PersonSearch({
  name,
  label,
  chosenName,
  hasValue,
  search: runSearch,
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
  search: (q: string, signal: AbortSignal) => Promise<{ matches: readonly PersonMatch[] }>;
  onPick: (match: PersonMatch) => void;
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
    queryKey: ["people-search", submitted],
    enabled: submitted.length >= MIN,
    queryFn: ({ signal }) => runSearch(submitted, signal),
  });

  if (hasValue) {
    return (
      <div className="flex flex-col gap-1">
        <span className="font-semibold">{label}</span>
        <div className="flex flex-wrap items-center gap-3">
          <span className="min-w-0 break-all text-lg">{chosenName}</span>
          <Button ref={changeRef} variant="secondary" disabled={disabled} onClick={onClear}>
            {t("shell.person_search.change")}
          </Button>
        </div>
      </div>
    );
  }

  const submit = () => {
    const v = term.trim();
    if (v.length >= MIN) setSubmitted(v);
  };
  const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") {
      e.preventDefault();
      submit();
    }
  };
  const error_ = isApiError(q.error) ? q.error : null;
  const matches = q.data?.matches ?? [];

  return (
    <div className="flex flex-col gap-3" role="search">
      <Field
        label={label}
        name={name}
        hint={t("shell.person_search.hint", { min: MIN })}
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
          onClick={submit}
        >
          {t("shell.person_search.search")}
        </Button>
      </div>
      {q.isFetching ? <Loading /> : null}
      {error_ ? (
        <ErrorState
          error={error_}
          {...(error_.code === "permission"
            ? { message: t("shell.person_search.forbidden") }
            : { onRetry: () => void q.refetch() })}
        />
      ) : null}
      <p role="status" className="text-sm text-muted">
        {q.data ? t("shell.person_search.found", { count: matches.length }) : ""}
      </p>
      {matches.length > 0 ? (
        <ul className="flex flex-col gap-2" aria-label={t("shell.person_search.results")}>
          {matches.map((m) => (
            <li key={m.id}>
              <button
                type="button"
                onClick={() => onPick(m)}
                className="flex min-h-11 w-full flex-wrap items-center gap-x-2 rounded-md border border-border bg-surface px-3 py-2 text-start"
              >
                <span className="font-semibold">
                  {m.playa_name || t("shell.person_search.unnamed")}
                </span>
                {m.pronouns ? <span className="text-muted">({m.pronouns})</span> : null}
              </button>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}
