import { useRef, useState } from "react";
import type { FormEvent } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { Link } from "react-router";
import { api, ApiError } from "@/api";
import { useFormat, useT } from "@/i18n";
import {
  Button,
  EmptyState,
  ErrorState,
  ErrorSummary,
  Field,
  Loading,
  Table,
  useFieldErrors,
} from "@/ui";
import type { Column } from "@/ui";
import { stateKeys } from "../accountState";
import type { Account } from "../accountState";
import { text } from "../text";

/** `/accounts/admin`: the newest accounts, or the one with an exact address. The searched
 *  address stays in this component (never in the URL: it is personal). */
export function AdminList() {
  const t = useT();
  const format = useFormat();
  const [search, setSearch] = useState("");
  const formRef = useRef<HTMLFormElement>(null);

  const q = useInfiniteQuery({
    queryKey: ["accounts", "list", search],
    initialPageParam: "",
    queryFn: ({ signal, pageParam }) =>
      api.GET("/api/accounts/", {
        signal,
        params: { query: search ? { email: search } : pageParam ? { after: pageParam } : {} },
      }),
    getNextPageParam: (last) => last.next || undefined,
  });

  const accounts = q.data?.pages.flatMap((p) => p.accounts) ?? [];
  const error = q.error instanceof ApiError ? q.error : null;
  const fieldErrors = useFieldErrors(error?.code === "invalid" ? error : null);
  const forbidden = error?.code === "permission";

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    setSearch(text(new FormData(e.currentTarget).get("email")).trim());
  };

  const columns: Column<Account>[] = [
    {
      header: t("core.accounts.col.email"),
      rowHeader: true,
      cell: (a) => (
        <Link
          to={`/accounts/admin/${a.id}`}
          className="inline-flex min-h-11 items-center break-all underline"
        >
          {a.email}
        </Link>
      ),
    },
    {
      header: t("core.accounts.col.state"),
      cell: (a) => format.list(stateKeys(a).map((k) => t(k))),
    },
    {
      header: t("core.accounts.col.last"),
      cell: (a) => (a.last_login_at ? format.dateTime(a.last_login_at) : t("core.accounts.never")),
    },
    { header: t("core.accounts.col.created"), cell: (a) => format.dateTime(a.created_at) },
  ];

  return (
    <div className="flex flex-col gap-4" aria-busy={q.isPending || undefined}>
      <h1 className="text-2xl font-bold">{t("core.accounts.admin.title")}</h1>
      {forbidden ? null : (
        <form
          ref={formRef}
          onSubmit={submit}
          className="flex flex-col gap-3"
          role="search"
          noValidate
        >
          <Field
            label={t("core.accounts.admin.search")}
            hint={t("core.accounts.admin.search_hint")}
            name="email"
            type="email"
            inputMode="email"
            autoComplete="off"
            error={fieldErrors["email"]}
          />
          <div className="flex flex-wrap gap-3">
            <Button type="submit">{t("core.accounts.admin.search_submit")}</Button>
            {search ? (
              <Button
                variant="secondary"
                onClick={() => {
                  formRef.current?.reset();
                  setSearch("");
                }}
              >
                {t("core.accounts.admin.search_clear")}
              </Button>
            ) : null}
          </div>
        </form>
      )}

      {q.isPending ? <Loading /> : null}
      {error?.code === "invalid" ? (
        <ErrorSummary error={error} formRef={formRef} />
      ) : error ? (
        <ErrorState
          error={error}
          {...(forbidden
            ? { message: t("core.accounts.admin.forbidden") }
            : { onRetry: () => void q.refetch() })}
        />
      ) : null}
      {q.data && accounts.length === 0 ? (
        <EmptyState>
          {search ? t("core.accounts.admin.search_none") : t("core.accounts.admin.empty")}
        </EmptyState>
      ) : null}
      {accounts.length > 0 ? (
        <Table
          caption={t("core.accounts.admin.caption")}
          columns={columns}
          rows={accounts}
          rowKey={(a) => a.id}
        />
      ) : null}
      {q.hasNextPage ? (
        <div>
          <Button
            variant="secondary"
            busy={q.isFetchingNextPage}
            onClick={() => void q.fetchNextPage()}
          >
            {t("core.accounts.admin.more")}
          </Button>
        </div>
      ) : null}
    </div>
  );
}
