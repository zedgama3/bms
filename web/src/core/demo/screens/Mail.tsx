import { useId, useState } from "react";
import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { ApiError, demoApi, useSession } from "@/api";
import type { DemoMail } from "@/api";
import { useFormat, useT } from "@/i18n";
import { Button, EmptyState, ErrorState, Loading } from "@/ui";

const URL_RE = /https?:\/\/[^\s<>"']+/g;

/** The body as plain text with web addresses made clickable (a tester completes a password
 *  reset from here). Only http and https; trailing punctuation stays outside the link. */
function linkify(body: string): ReactNode[] {
  const out: ReactNode[] = [];
  let last = 0;
  for (const m of body.matchAll(URL_RE)) {
    const start = m.index;
    const href = m[0].replace(/[.,;:!?)\]]+$/, "");
    if (start > last) out.push(body.slice(last, start));
    out.push(
      <a key={start} href={href} className="break-all underline">
        {href}
      </a>,
    );
    last = start + href.length;
  }
  if (last < body.length) out.push(body.slice(last));
  return out;
}

/** `/demo/mail`: the mail the demo captured for the signed-in account's own address, newest
 *  first. States: loading, empty, off (`501`), error with reference and retry; a signed-out
 *  visit never reaches it (the frame sends it to sign-in, and so does a `401`). */
export function Mail() {
  const t = useT();
  const session = useSession();
  const personId = session.signedIn ? session.personId : "";
  const q = useQuery({
    queryKey: ["demo", "mail", personId],
    enabled: session.signedIn,
    queryFn: ({ signal }) => demoApi.GET("/api/demo/mail", { signal }),
  });
  const error = q.error instanceof ApiError ? q.error : null;
  const mail = q.data?.mail ?? [];

  let body: ReactNode;
  if (error?.code === "unsupported" || error?.code === "not_found" || error?.status === 501) {
    body = <EmptyState>{t("core.demo.mail.off")}</EmptyState>;
  } else if (error) {
    body = <ErrorState error={error} onRetry={() => void q.refetch()} />;
  } else if (q.isPending) {
    body = <Loading />;
  } else if (mail.length === 0) {
    body = <EmptyState>{t("core.demo.mail.empty")}</EmptyState>;
  } else {
    body = (
      <ul aria-label={t("core.demo.mail.list")} className="flex flex-col gap-3">
        {mail.map((m) => (
          <Message key={m.id} mail={m} />
        ))}
      </ul>
    );
  }

  return (
    <section aria-labelledby="demo-mail-title" aria-busy={q.isPending || undefined}>
      <div className="mx-auto flex max-w-2xl flex-col gap-4">
        <h1 id="demo-mail-title" className="text-2xl font-bold">
          {t("core.demo.mail.title")}
        </h1>
        <p>{t("core.demo.mail.note")}</p>
        <div>
          <Button variant="secondary" busy={q.isFetching} onClick={() => void q.refetch()}>
            {t("core.demo.mail.refresh")}
          </Button>
        </div>
        {body}
      </div>
    </section>
  );
}

/** One message: its subject is a disclosure button (touch and keyboard), the text opens below. */
function Message({ mail }: { mail: DemoMail }) {
  const t = useT();
  const format = useFormat();
  const [open, setOpen] = useState(false);
  const bodyId = useId();
  return (
    <li className="rounded-md border border-border">
      <button
        type="button"
        aria-expanded={open}
        aria-controls={bodyId}
        onClick={() => setOpen(!open)}
        className="flex min-h-11 w-full flex-col items-start gap-1 rounded-md px-3 py-2 text-start"
      >
        <span className="font-semibold">{mail.subject || t("core.demo.mail.no_subject")}</span>
        <span className="break-all text-sm text-muted">
          {t("core.demo.mail.to", { to: mail.to })}
        </span>
        <span className="text-sm text-muted">{format.relative(mail.captured_at)}</span>
      </button>
      <div id={bodyId} hidden={!open} className="border-t border-border px-3 py-3">
        <p className="whitespace-pre-wrap break-words">{linkify(mail.body)}</p>
      </div>
    </li>
  );
}
