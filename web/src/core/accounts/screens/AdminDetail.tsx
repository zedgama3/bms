import { useRef, useState } from "react";
import type { FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router";
import { api, ApiError, useReadOnly, useSession } from "@/api";
import { useFormat, useT } from "@/i18n";
import { Button, Dialog, ErrorState, ErrorSummary, Loading, useToast } from "@/ui";
import { stateKeys } from "../accountState";
import type { Account } from "../accountState";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { useAction } from "../useAction";

type Confirm = "disable" | "enable" | "temp" | null;

/** `/accounts/admin/:account`: one account, with disable or enable and a temporary password.
 *  The buttons say why they are off (own account, superuser account, read-only copy). */
export function AdminDetail() {
  const t = useT();
  const format = useFormat();
  const toast = useToast();
  const client = useQueryClient();
  const session = useSession();
  const { readOnly } = useReadOnly();
  const { account: id = "" } = useParams();
  const [confirm, setConfirm] = useState<Confirm>(null);
  const [secret, setSecret] = useState("");
  const confirmForm = useRef<HTMLFormElement>(null);
  const act = useAction();

  const q = useQuery({
    queryKey: ["accounts", "account", id],
    queryFn: ({ signal }) =>
      api.GET("/api/accounts/{account}", { signal, params: { path: { account: id } } }),
  });
  const error = q.error instanceof ApiError ? q.error : null;

  if (!q.data && q.isPending) {
    return (
      <div aria-busy="true">
        <h1 className="mb-4 text-2xl font-bold">{t("core.accounts.detail.title")}</h1>
        <Loading />
      </div>
    );
  }
  // A failed background refetch keeps the account (and any open dialog) on screen.
  if (!q.data) {
    return (
      <div className="flex flex-col gap-4">
        <h1 className="text-2xl font-bold">{t("core.accounts.detail.title")}</h1>
        {error ? (
          <ErrorState
            error={error}
            {...(error.code === "permission" ? {} : { onRetry: () => void q.refetch() })}
            {...(error.code === "permission"
              ? { message: t("core.accounts.admin.forbidden") }
              : error.code === "invalid"
                ? { message: t("error.not_found") } // a malformed ID in the path
                : {})}
          />
        ) : null}
        <BackLink />
      </div>
    );
  }

  const a: Account = q.data;
  const own = session.signedIn && session.accountId === a.id;
  const reason = own
    ? t("core.accounts.detail.reason_self")
    : a.superuser
      ? t("core.accounts.detail.reason_superuser")
      : "";
  const off = readOnly || reason !== "";
  const disabled = Boolean(a.disabled_at);
  const describedBy = [reason ? "account-reason" : "", readOnly ? "account-readonly" : ""]
    .filter(Boolean)
    .join(" ");
  const offProps = off
    ? { disabled: true, ...(describedBy ? { "aria-describedby": describedBy } : {}) }
    : {};

  const done = () => {
    // Not awaited: the person sees the result (and a temporary password) at once.
    void client.invalidateQueries({ queryKey: ["accounts"] });
  };

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const what = confirm;
    if (what === null) return;
    const path = { params: { path: { account: id } } };
    void act.run(async () => {
      if (what === "disable") {
        await api.POST("/api/accounts/{account}/disable", path);
        toast.show(t("core.accounts.detail.disabled"));
      } else if (what === "enable") {
        await api.POST("/api/accounts/{account}/enable", path);
        toast.show(t("core.accounts.detail.enabled"));
      } else {
        const res = await api.POST("/api/accounts/{account}/temporary-password", path);
        // The confirmation closes (setConfirm(null) below) in the same update that opens the
        // password dialog. This relies on the confirmation <Dialog> coming BEFORE the password
        // <Dialog> in the JSX, so its close runs first and focus lands sensibly: do not swap
        // them. The password is held in this component's state only: never in the query
        // cache, never stored.
        setSecret(res.password);
      }
      setConfirm(null);
      done();
    });
  };

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(secret);
      toast.show(t("core.accounts.detail.temp_copied"));
    } catch {
      // No clipboard (an insecure page, a denied permission): the password is on screen to read.
    }
  };

  const titleKey =
    confirm === "disable"
      ? "core.accounts.detail.disable_title"
      : confirm === "enable"
        ? "core.accounts.detail.enable_title"
        : "core.accounts.detail.temp_title";
  const bodyKey =
    confirm === "disable"
      ? "core.accounts.detail.disable_body"
      : confirm === "enable"
        ? "core.accounts.detail.enable_body"
        : "core.accounts.detail.temp_body";
  const confirmKey =
    confirm === "disable"
      ? "core.accounts.detail.disable"
      : confirm === "enable"
        ? "core.accounts.detail.enable"
        : "core.accounts.detail.temp_confirm";

  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-2xl font-bold">{t("core.accounts.detail.title")}</h1>
      {error ? <ErrorState error={error} onRetry={() => void q.refetch()} /> : null}
      <dl className="grid grid-cols-1 gap-3 rounded-md border border-border p-4 sm:grid-cols-[auto_1fr] sm:gap-x-6">
        <Row label={t("core.accounts.col.email")} value={a.email} wrap />
        <Row
          label={t("core.accounts.col.state")}
          value={format.list(stateKeys(a).map((k) => t(k)))}
        />
        <Row
          label={t("core.accounts.col.last")}
          value={a.last_login_at ? format.dateTime(a.last_login_at) : t("core.accounts.never")}
        />
        <Row label={t("core.accounts.col.created")} value={format.dateTime(a.created_at)} />
        <Row
          label={t("core.accounts.detail.changed")}
          value={format.dateTime(a.password_changed_at)}
        />
      </dl>

      {reason ? (
        <p id="account-reason" className="font-semibold">
          {reason}
        </p>
      ) : null}
      <ReadOnlyNote id="account-readonly" readOnly={readOnly} />
      <div className="flex flex-wrap gap-3">
        <Button
          variant={disabled ? "primary" : "danger"}
          {...offProps}
          onClick={() => {
            act.clear();
            setConfirm(disabled ? "enable" : "disable");
          }}
        >
          {disabled ? t("core.accounts.detail.enable") : t("core.accounts.detail.disable")}
        </Button>
        <Button
          variant="secondary"
          {...offProps}
          onClick={() => {
            act.clear();
            setConfirm("temp");
          }}
        >
          {t("core.accounts.detail.temp")}
        </Button>
      </div>
      <BackLink />

      <Dialog open={confirm !== null} title={t(titleKey)} onClose={() => setConfirm(null)}>
        <form ref={confirmForm} onSubmit={submit} className="flex flex-col gap-4">
          <ErrorSummary error={act.error} formRef={confirmForm} />
          <p>{t(bodyKey)}</p>
          <div className="flex flex-wrap gap-3">
            <Button
              type="submit"
              variant={confirm === "disable" ? "danger" : "primary"}
              busy={act.busy}
            >
              {t(confirmKey)}
            </Button>
            <Button variant="secondary" onClick={() => setConfirm(null)}>
              {t("core.accounts.cancel")}
            </Button>
          </div>
        </form>
      </Dialog>

      <Dialog
        open={secret !== ""}
        title={t("core.accounts.detail.temp_done_title")}
        onClose={() => setSecret("")}
      >
        <div className="flex flex-col gap-4">
          <p>{t("core.accounts.detail.temp_done_body")}</p>
          <p className="font-semibold">{t("core.accounts.detail.temp_label")}</p>
          <p>
            <code className="block break-all rounded-md border border-border bg-surface p-3 text-lg">
              {secret}
            </code>
          </p>
          <p>{t("core.accounts.detail.temp_must")}</p>
          <div className="flex flex-wrap gap-3">
            <Button onClick={() => void copy()}>{t("core.accounts.detail.temp_copy")}</Button>
          </div>
        </div>
      </Dialog>
    </div>
  );
}

function BackLink() {
  const t = useT();
  return (
    <div>
      <Link to="/accounts/admin" className="inline-flex min-h-11 items-center underline">
        {t("core.accounts.detail.back")}
      </Link>
    </div>
  );
}

function Row({ label, value, wrap = false }: { label: string; value: string; wrap?: boolean }) {
  return (
    <div className="contents">
      <dt className="font-semibold">{label}</dt>
      <dd className={wrap ? "break-all" : ""}>{value}</dd>
    </div>
  );
}
