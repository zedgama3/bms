import { useRef } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, useSession } from "@/api";
import { isMessageKey, useT } from "@/i18n";
import { Button, ErrorState, ErrorSummary, Loading, useToast } from "@/ui";

/** `/hello/`: the reference screen. A public part (greeting and notices from other modules)
 *  and a signed-in part (your count, the server's count, a button). */
export function Greeting() {
  const t = useT();
  const session = useSession();
  const toast = useToast();
  const client = useQueryClient();
  const formRef = useRef<HTMLFormElement>(null);

  const pub = useQuery({
    queryKey: ["hello", "public-greeting"],
    queryFn: ({ signal }) => api.GET("/api/hello/public-greeting", { signal }),
  });
  const mine = useQuery({
    queryKey: ["hello", "greeting"],
    queryFn: ({ signal }) => api.GET("/api/hello/greeting", { signal }),
    enabled: session.signedIn,
  });
  const greet = useMutation({
    mutationFn: () => api.POST("/api/hello/greetings"),
    onSuccess: async () => {
      toast.show(t("module.hello.screen.greeted"));
      await client.invalidateQueries({ queryKey: ["hello"] });
    },
  });

  // A banner's key names another module's catalogue: narrow it with the runtime guard and
  // drop one that is not in a loaded catalogue (no untranslated string is ever shown).
  const banners = (pub.data?.banners ?? []).flatMap((b) =>
    isMessageKey(b.key) ? [{ ...b, key: b.key }] : [],
  );

  return (
    <div className="flex flex-col gap-6" aria-busy={pub.isPending || undefined}>
      <h1 className="text-2xl font-bold">
        {pub.data
          ? t("module.hello.screen.greeting", { greeting: pub.data.greeting })
          : t("module.hello.title")}
      </h1>

      {pub.isPending ? <Loading /> : null}
      {pub.error instanceof ApiError ? (
        <ErrorState error={pub.error} onRetry={() => void pub.refetch()} />
      ) : null}
      {banners.length > 0 ? (
        <section aria-labelledby="hello-banners">
          <h2 id="hello-banners" className="sr-only">
            {t("module.hello.screen.banners")}
          </h2>
          <ul className="flex flex-col gap-2">
            {banners.map((b) => (
              <li
                key={`${b.module}:${b.key}`}
                className="rounded-md border border-border bg-surface p-3"
              >
                {t(b.key)}
              </li>
            ))}
          </ul>
        </section>
      ) : null}

      {session.signedIn ? (
        <form
          ref={formRef}
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (!greet.isPending) greet.mutate();
          }}
        >
          {mine.isPending ? <Loading /> : null}
          {mine.error instanceof ApiError ? (
            <ErrorState error={mine.error} onRetry={() => void mine.refetch()} />
          ) : null}
          {mine.data ? (
            <div className="flex flex-col gap-1">
              <p>{t("module.hello.screen.mine", { mine: mine.data.mine ?? 0 })}</p>
              <p>{t("module.hello.screen.seen", { seen: mine.data.seen ?? 0 })}</p>
            </div>
          ) : null}
          <ErrorSummary
            error={greet.error instanceof ApiError ? greet.error : null}
            formRef={formRef}
          />
          <div>
            <Button type="submit" busy={greet.isPending}>
              {t("module.hello.screen.greet")}
            </Button>
          </div>
        </form>
      ) : null}
    </div>
  );
}
