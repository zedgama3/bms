import { useInfiniteQuery } from "@tanstack/react-query";
import type { QueryKey } from "@tanstack/react-query";
import { ApiError } from "@/api";
import { useT } from "@/i18n";
import { Button } from "@/ui";
import type { Paged } from "./calls";
import { ScreenError } from "./ScreenError";

/** A long list read a page at a time: the rows so far, **Show more** while the server's `next`
 *  cursor is present (phone first: no page numbers). The cursor is opaque and kept in memory
 *  only. */
export function usePaged<T>(
  queryKey: QueryKey,
  fetchPage: (after: string, signal: AbortSignal) => Promise<Paged<T>>,
  enabled = true,
) {
  const q = useInfiniteQuery({
    queryKey,
    enabled,
    initialPageParam: "",
    queryFn: ({ pageParam, signal }) => fetchPage(pageParam, signal),
    getNextPageParam: (last) => (last.next === "" ? undefined : last.next),
  });
  return {
    q,
    /** Every row loaded so far, in order. */
    rows: q.data?.pages.flatMap((p) => p.items) ?? [],
    /** The first page's failure, or a failed background refetch of loaded pages (a screen error, shown
     *  beside the rows so stale rows are not taken for current ones) and a later page's (shown by `ShowMore`). */
    error:
      q.error instanceof ApiError && (!q.data || (q.isRefetchError && !q.isFetchNextPageError))
        ? q.error
        : null,
  };
}

/** The **Show more** button under a paged list; a failed next page is said in place with a
 *  retry, never silently. */
export function ShowMore({
  q,
}: {
  q: {
    hasNextPage: boolean;
    isFetchingNextPage: boolean;
    isFetchNextPageError: boolean;
    error: unknown;
    fetchNextPage: () => unknown;
  };
}) {
  const t = useT();
  const laterError = q.isFetchNextPageError && q.error instanceof ApiError ? q.error : null;
  if (!q.hasNextPage && !laterError) return null;
  return (
    <div className="flex flex-col gap-3">
      {laterError ? (
        <ScreenError error={laterError} onRetry={() => void q.fetchNextPage()} />
      ) : null}
      {q.hasNextPage && !laterError ? (
        <div>
          <Button
            variant="secondary"
            busy={q.isFetchingNextPage}
            onClick={() => void q.fetchNextPage()}
          >
            {t("module.volunteers.show_more")}
          </Button>
        </div>
      ) : null}
    </div>
  );
}
