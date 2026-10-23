import { useEffect, useRef, useState } from "react";
import type { ApiError } from "@/api";
import { useT } from "@/i18n";
import { Button } from "./Button";
import { ErrorState } from "./errors";

/** One file row. `fetch` is the screen's call through `@/api` (the kit makes no request of its
 *  own): it resolves with the bytes, or rejects with an `ApiError`. */
export interface AttachmentRow {
  id: string;
  /** What the row says. Never a raw file name chosen by someone else, unless the screen has
   *  decided that is fit to show. */
  label: string;
  fetch: (signal: AbortSignal) => Promise<Blob>;
  /** Present when the screen enables removal: shows a remove button that calls it. */
  onRemove?: () => void;
}

type Failure =
  | { kind: "gone" }
  | { kind: "not_ready" }
  | { kind: "unexpected" }
  | { kind: "error"; error: ApiError };

function failureOf(e: unknown): Failure | null {
  if (typeof e === "object" && e !== null && (e as { name?: unknown }).name === "AbortError")
    return null;
  if (typeof e !== "object" || e === null || (e as { name?: unknown }).name !== "ApiError") {
    return { kind: "unexpected" };
  }
  const error = e as ApiError;
  if (error.status === 404) return { kind: "gone" };
  if (error.status === 503 && error.retryAfter !== undefined) return { kind: "not_ready" };
  return { kind: "error", error };
}

/** A list of file rows with an **Open** button (and a remove button when the screen enables
 *  it). Opening fetches through the screen's `fetch`, then offers a link that shows the bytes in a new tab; a `404`
 *  says the file is no longer available, a `503` with `Retry-After` says it is not available on
 *  this copy yet, anything else is an `ErrorState`. Touch alone is enough. */
export function Attachments({
  rows,
  goneMessage,
}: {
  rows: readonly AttachmentRow[];
  /** Replaces "This file is no longer available" (the screen names what the file is). */
  goneMessage?: string;
}) {
  return (
    <ul className="flex flex-col gap-2">
      {rows.map((row) => (
        <Row key={row.id} row={row} {...(goneMessage ? { goneMessage } : {})} />
      ))}
    </ul>
  );
}

function Row({ row, goneMessage }: { row: AttachmentRow; goneMessage?: string }) {
  const t = useT();
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<Failure | null>(null);
  const [href, setHref] = useState<string | null>(null);
  const controller = useRef<AbortController | null>(null);
  const link = useRef<HTMLAnchorElement | null>(null);

  useEffect(() => () => controller.current?.abort(), []);

  // The object URL lives as long as it is shown: replaced or unmounted, it is revoked.
  useEffect(() => {
    if (href === null) return;
    link.current?.focus();
    return () => URL.revokeObjectURL(href);
  }, [href]);

  const open = () => {
    if (busy) return;
    setBusy(true);
    setFailure(null);
    setHref(null);
    controller.current = new AbortController();
    row
      .fetch(controller.current.signal)
      // A new tab opened after the await would be blocked on phones as a popup: show a real
      // link instead, so the tab comes from the person's own tap.
      .then((blob) => setHref(URL.createObjectURL(blob)))
      .catch((e: unknown) => setFailure(failureOf(e)))
      .finally(() => setBusy(false));
  };

  return (
    <li className="flex flex-col gap-2 rounded-md border border-border bg-surface p-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <span className="break-words font-semibold">{row.label}</span>
        <span className="flex flex-wrap gap-3">
          <Button variant="secondary" busy={busy} onClick={open}>
            {t("shell.attachments.open")}
            <span className="sr-only"> {row.label}</span>
          </Button>
          {row.onRemove ? (
            <Button variant="danger" onClick={row.onRemove}>
              {t("shell.attachments.remove")}
              <span className="sr-only"> {row.label}</span>
            </Button>
          ) : null}
        </span>
      </div>
      {href !== null ? (
        <p role="status" className="flex flex-col gap-1">
          <span>{t("shell.attachments.ready")}</span>
          <a
            ref={link}
            href={href}
            target="_blank"
            rel="noopener"
            className="inline-flex min-h-11 items-center font-semibold underline"
          >
            {t("shell.attachments.open_link", { label: row.label })}
          </a>
        </p>
      ) : null}
      {failure?.kind === "gone" ? (
        <p role="alert" className="font-semibold text-danger">
          {goneMessage ?? t("shell.attachments.gone")}
        </p>
      ) : null}
      {failure?.kind === "not_ready" ? (
        <p role="alert" className="font-semibold text-warn-fg">
          {t("shell.attachments.not_ready")}
        </p>
      ) : null}
      {failure?.kind === "unexpected" ? (
        <div role="alert" className="flex flex-col items-start gap-2">
          <p className="font-semibold text-danger">{t("error.internal")}</p>
          <Button variant="secondary" onClick={open}>
            {t("shell.retry")}
          </Button>
        </div>
      ) : null}
      {failure?.kind === "error" ? <ErrorState error={failure.error} onRetry={open} /> : null}
    </li>
  );
}
