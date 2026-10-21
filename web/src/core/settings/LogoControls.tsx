import { useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { api, ApiError, upload, useReadOnly, usePublic } from "@/api";
import type { components } from "@/api";
import { useT } from "@/i18n";
import {
  Banner,
  Button,
  Dialog,
  EmptyState,
  ErrorSummary,
  FileField,
  useFieldErrors,
  useToast,
} from "@/ui";
import { byName, focusSoon } from "./focus";
import { useRefreshPublic } from "./queries";
import { ReadOnlyNote } from "./ReadOnlyNote";
import { useAction } from "./useAction";

/** The route's cap and the logo's formats (settings.md "The logo field"): the server enforces
 *  both; the field checks the size first so a phone does not send 2 MB for nothing. */
export const MAX_LOGO_BYTES = 2_097_152;
export const LOGO_TYPES = ["image/png", "image/jpeg", "image/webp"] as const;
const LOGO_ROUTE = "/api/settings/logo";

type LogoResult = components["schemas"]["SettingsLogoResult"];

/** Puts what the server meant in the shape the field shows: a 413 (the body past the route cap)
 *  is `file: too_large {max}`; a 409 without a field is the site's file quota. */
function asFieldError(e: ApiError | null): ApiError | null {
  if (e === null) return null;
  const init = {
    method: e.method,
    route: e.route,
    module: e.module,
    traceId: e.traceId,
    ref: e.ref,
  };
  if (e.code === "payload_too_large") {
    return new ApiError({
      ...init,
      status: 400,
      code: "invalid",
      fields: [
        {
          field: "file",
          code: "too_large",
          params: { max: String(MAX_LOGO_BYTES) },
          reason: "",
        },
      ],
    });
  }
  if (e.code === "conflict" && !e.fields.some((f) => f.field === "file")) {
    return new ApiError({
      ...init,
      status: e.status,
      code: "conflict",
      fields: [{ field: "file", code: "quota", reason: "" }],
    });
  }
  return e;
}

/** The Logo section of `/settings` (settings.md "The logo field"): the preview or the empty
 *  state; with `canManage` and a writing copy, a file picker with an explicit Upload or Replace,
 *  and Remove logo behind a confirmation. Its requests are its own, not part of Save. */
export function LogoControls({ canManage }: { canManage: boolean }) {
  const t = useT();
  const toast = useToast();
  const { name, logo } = usePublic();
  const refreshPublic = useRefreshPublic();
  const { readOnly } = useReadOnly();
  const formRef = useRef<HTMLFormElement>(null);
  const dialogForm = useRef<HTMLFormElement>(null);
  const removeBtn = useRef<HTMLButtonElement>(null);
  const [file, setFile] = useState<File | null>(null);
  const [confirming, setConfirming] = useState(false);
  // The file ID whose image did not load, so a replaced logo is tried again.
  const [failed, setFailed] = useState("");
  const putAct = useAction();
  const delAct = useAction();

  // The upload's error is the form's; the removal's stays in its dialog. Starting one clears
  // the other's, so a stale message never sits beside a new action.
  const error = asFieldError(putAct.error);
  const fieldErrors = useFieldErrors(error);
  const readOnlyError = error?.code === "read_only";
  // 501: no file store here, so there is nothing to upload to; the controls give way to a note.
  const noStore = error?.code === "unsupported" || delAct.error?.code === "unsupported";

  // The form (or the dialog) unmounts under focus: land on the heading so the place is kept.
  useEffect(() => {
    if (noStore) document.getElementById("settings-logo")?.focus();
  }, [noStore]);

  const send = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (file === null) return;
    delAct.reset();
    void putAct
      .run(async () => {
        await upload<LogoResult>(LOGO_ROUTE, { file }, { method: "PUT" });
        await refreshPublic();
        setFile(null);
        toast.show(t("core.settings.logo.uploaded"));
      })
      .then((ok) => {
        if (ok) focusSoon(byName("file"));
      });
  };

  const pickFile = (f: File | null) => {
    putAct.reset();
    setFile(f);
  };

  const remove = (e?: FormEvent<HTMLFormElement>) => {
    e?.preventDefault();
    putAct.reset();
    void delAct
      .run(async () => {
        await api.DELETE(LOGO_ROUTE);
        await refreshPublic();
        toast.show(t("core.settings.logo.removed"));
      })
      .then((ok) => {
        // A failure stays in the dialog, next to the button that was pressed.
        if (!ok) return;
        setConfirming(false);
        // The button that opened the dialog is gone: keep the person's place at the picker.
        focusSoon(byName("file"));
      });
  };

  // Cancel or Esc: back to the button that opened the dialog (the browser does this too; this
  // keeps it true where the dialog's own focus restore is missing).
  const cancel = () => {
    delAct.reset();
    setConfirming(false);
    setTimeout(() => removeBtn.current?.focus(), 0);
  };

  const showControls = canManage && !readOnly && !noStore;
  const hasLogo = logo !== "";

  return (
    <section aria-labelledby="settings-logo" className="flex flex-col gap-4">
      <h2 id="settings-logo" tabIndex={-1} className="text-xl font-bold">
        {t("core.settings.logo.heading")}
      </h2>
      {hasLogo ? (
        failed === logo ? (
          <p>{t("core.settings.logo.unavailable")}</p>
        ) : (
          <img
            src={`${LOGO_ROUTE}?v=${encodeURIComponent(logo)}`}
            alt={t("core.settings.logo.current_alt")}
            decoding="async"
            className="max-h-24 w-auto max-w-full self-start"
            onError={() => setFailed(logo)}
          />
        )
      ) : (
        <EmptyState>{t("core.settings.logo.none", { name })}</EmptyState>
      )}
      {noStore ? <Banner tone="warn">{t("core.settings.logo.unsupported")}</Banner> : null}
      {showControls ? (
        <form ref={formRef} onSubmit={send} noValidate className="flex flex-col gap-4">
          {readOnlyError ? <ReadOnlyNote id="settings-logo-readonly" readOnly /> : null}
          {!readOnlyError ? <ErrorSummary error={error} formRef={formRef} /> : null}
          <FileField
            label={t("core.settings.logo.field")}
            name="file"
            hint={t("core.settings.logo.hint")}
            error={fieldErrors["file"]}
            accept={LOGO_TYPES}
            maxBytes={MAX_LOGO_BYTES}
            value={file}
            onChange={pickFile}
            disabled={putAct.busy}
          />
          <div className="flex flex-wrap gap-3">
            <Button
              type="submit"
              busy={putAct.busy}
              disabled={file === null}
              {...(readOnlyError ? { "aria-describedby": "settings-logo-readonly" } : {})}
            >
              {hasLogo ? t("core.settings.logo.replace") : t("core.settings.logo.upload")}
            </Button>
            {hasLogo ? (
              <Button
                ref={removeBtn}
                variant="danger"
                onClick={() => {
                  delAct.reset();
                  setConfirming(true);
                }}
              >
                {t("core.settings.logo.remove")}
              </Button>
            ) : null}
          </div>
        </form>
      ) : null}
      <Dialog open={confirming && !noStore} title={t("core.settings.logo.remove")} onClose={cancel}>
        <form ref={dialogForm} onSubmit={remove} className="flex flex-col gap-4">
          <ErrorSummary error={delAct.error} formRef={dialogForm} />
          <p>{t("core.settings.logo.remove_confirm")}</p>
          <div className="flex flex-wrap gap-3">
            <Button type="submit" variant="danger" busy={delAct.busy}>
              {t("core.settings.logo.remove_yes")}
            </Button>
            <Button variant="secondary" onClick={cancel}>
              {t("core.settings.logo.cancel")}
            </Button>
          </div>
        </form>
      </Dialog>
    </section>
  );
}
