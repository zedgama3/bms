import { useRef, useState } from "react";
import type { FormEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";
import { api, ApiError, useReadOnly, useSession } from "@/api";
import { useT } from "@/i18n";
import { Button, EmptyState, ErrorState, Field, linkButtonClass, Loading } from "@/ui";
import { holds } from "../helpers";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { Forbidden } from "../ScreenError";
import { text } from "../text";

const MIN = 2;

/** `/people`: search people by playa name. The term stays in this component (never in the URL
 *  or storage: a search can match personal details). */
export function People() {
  const t = useT();
  const session = useSession();
  const { readOnly } = useReadOnly();
  const noAccess = !holds(session, "people.directory.read");
  const [term, setTerm] = useState("");
  const [short, setShort] = useState(false);
  const formRef = useRef<HTMLFormElement>(null);

  const q = useQuery({
    queryKey: ["people", "search", term],
    enabled: term.length >= MIN && !noAccess,
    queryFn: ({ signal }) =>
      api.GET("/api/people/search", { signal, params: { query: { q: term } } }),
  });
  const error = q.error instanceof ApiError ? q.error : null;
  const forbidden = noAccess || error?.code === "permission";
  const matches = q.data?.matches ?? [];
  const canCreate = holds(session, "people.profiles.manage");

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const v = text(new FormData(e.currentTarget).get("q")).trim();
    setShort(v.length < MIN);
    if (v.length >= MIN) setTerm(v);
  };

  return (
    <div className="flex flex-col gap-4" aria-busy={q.isFetching || undefined}>
      <h1 className="text-2xl font-bold">{t("core.people.search.title")}</h1>
      {noAccess ? <Forbidden /> : null}
      {canCreate ? (
        readOnly ? (
          <div className="flex flex-col gap-1">
            <div>
              <Button disabled aria-describedby="people-readonly">
                {t("core.people.search.new")}
              </Button>
            </div>
            <ReadOnlyNote id="people-readonly" readOnly />
          </div>
        ) : (
          <div>
            <Link to="/people/new" className={linkButtonClass("secondary")}>
              {t("core.people.search.new")}
            </Link>
          </div>
        )
      ) : null}
      {forbidden ? null : (
        <form
          ref={formRef}
          onSubmit={submit}
          role="search"
          className="flex flex-col gap-3"
          noValidate
        >
          <Field
            label={t("core.people.search.label")}
            name="q"
            type="search"
            autoComplete="off"
            maxLength={100}
            hint={t("core.people.search.hint", { min: MIN })}
            error={short ? { text: t("core.people.search.short", { min: MIN }) } : undefined}
          />
          <div>
            <Button type="submit">{t("core.people.search.submit")}</Button>
          </div>
        </form>
      )}
      {q.isFetching ? <Loading /> : null}
      {error && !noAccess ? (
        <ErrorState
          error={error}
          {...(error.code === "permission"
            ? { message: t("shell.forbidden") }
            : error.code === "invalid"
              ? {}
              : { onRetry: () => void q.refetch() })}
        />
      ) : null}
      <p role="status" className="text-sm text-muted">
        {q.data ? t("core.people.search.found", { count: matches.length }) : ""}
      </p>
      {q.data && matches.length === 0 ? (
        <EmptyState>{t("core.people.search.none")}</EmptyState>
      ) : null}
      {matches.length > 0 ? (
        <ul
          className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3"
          aria-label={t("core.people.search.results")}
        >
          {matches.map((m) => (
            <li key={m.id}>
              <Link
                to={`/people/${m.id}`}
                className="flex min-h-11 flex-col justify-center rounded-md border border-border bg-surface p-4"
              >
                <span className="break-words text-lg font-semibold">
                  {m.playa_name || t("core.people.search.unnamed")}
                </span>
                {m.pronouns ? <span className="text-muted">{m.pronouns}</span> : null}
              </Link>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}
