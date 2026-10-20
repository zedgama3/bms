import { useEffect, useRef, useState } from "react";
import { Link } from "react-router";
import { api, useReadOnly } from "@/api";
import { useT } from "@/i18n";
import { Button, ErrorState, linkButtonClass } from "@/ui";
import { ReadOnlyNote } from "./ReadOnlyNote";
import { useAction } from "./useAction";

/** "Send me a test": queues the `notify.test` message to the signed-in person and shows its ID
 *  (never an address). Only that template can be sent from here (notify decision 18). */
export function SendTest({ canOutbox }: { canOutbox: boolean }) {
  const t = useT();
  const { readOnly } = useReadOnly();
  const action = useAction();
  const [queued, setQueued] = useState<string | null>(null);
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (queued !== null) box.current?.focus();
  }, [queued]);

  const send = () => {
    setQueued(null);
    void action.run(async () => {
      const ref = await api.POST("/api/notify/test");
      setQueued(ref?.id ?? "");
    });
  };

  return (
    <section aria-labelledby="notify-test-title" className="flex flex-col gap-3">
      <h2 id="notify-test-title" className="text-xl font-bold">
        {t("core.notify.test.title")}
      </h2>
      <p>{t("core.notify.test.body")}</p>
      <ReadOnlyNote id="notify-test-readonly" readOnly={readOnly} />
      <div>
        <Button
          variant="secondary"
          busy={action.busy}
          disabled={readOnly}
          {...(readOnly ? { "aria-describedby": "notify-test-readonly" } : {})}
          onClick={send}
        >
          {t("core.notify.test.send")}
        </Button>
      </div>
      {action.error ? (
        <>
          <ErrorState
            error={action.error}
            {...(action.error.code === "unsupported"
              ? { message: t("core.notify.not_configured") }
              : {})}
          />
          {action.error.code === "unsupported" ? (
            <div>
              <Link to="/settings/notify" className={linkButtonClass("secondary")}>
                {t("core.notify.open_settings")}
              </Link>
            </div>
          ) : null}
        </>
      ) : null}
      {queued !== null ? (
        <div
          ref={box}
          role="status"
          tabIndex={-1}
          className="flex flex-col items-start gap-2 focus:outline-none"
        >
          <p>{t("core.notify.test.queued")}</p>
          <p className="break-all">{t("core.notify.test.id", { id: queued })}</p>
          {canOutbox ? (
            <Link to="/notify/outbox" className={linkButtonClass("secondary")}>
              {t("core.notify.test.open_outbox")}
            </Link>
          ) : null}
        </div>
      ) : null}
    </section>
  );
}
