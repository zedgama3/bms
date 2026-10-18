import { useRef, useState } from "react";
import type { FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, useReadOnly, useSession } from "@/api";
import { useT } from "@/i18n";
import {
  Button,
  Check,
  controlClass,
  Dialog,
  EmptyState,
  ErrorSummary,
  Field,
  Loading,
  useFieldErrors,
  useToast,
} from "@/ui";
import { focusSoon } from "../focus";
import { holds, useTitles } from "../helpers";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { ScreenError } from "../ScreenError";
import { Tabs } from "../Tabs";
import { text } from "../text";
import type { Rule } from "../types";
import { useAction } from "../useAction";

const LEVELS = ["internal", "personal", "sensitive"] as const;

/** `/access/rules`: separation-of-duties rules ("holding this role may never hold that
 *  permission"), how many people each affects now, and a form to add one. */
export function RuleList() {
  const t = useT();
  const titles = useTitles();
  const toast = useToast();
  const client = useQueryClient();
  const session = useSession();
  const { readOnly } = useReadOnly();
  const formRef = useRef<HTMLFormElement>(null);
  const delRef = useRef<HTMLFormElement>(null);
  const create = useAction();
  const del = useAction();
  const [kind, setKind] = useState<"permission" | "reaches">("permission");
  const [deleting, setDeleting] = useState<Rule | null>(null);
  const fieldErrors = useFieldErrors(create.error);

  const rulesQ = useQuery({
    queryKey: ["access", "rules"],
    queryFn: ({ signal }) => api.GET("/api/access/rules", { signal }),
  });
  const rolesQ = useQuery({
    queryKey: ["access", "roles"],
    queryFn: ({ signal }) => api.GET("/api/access/roles", { signal }),
  });
  const catQ = useQuery({
    queryKey: ["access", "permissions"],
    queryFn: ({ signal }) => api.GET("/api/access/permissions", { signal }),
  });
  const error = rulesQ.error instanceof ApiError ? rulesQ.error : null;
  const loadError =
    (rolesQ.error instanceof ApiError ? rolesQ.error : null) ??
    (catQ.error instanceof ApiError ? catQ.error : null);
  const canManage = holds(session, "access.rules.manage");
  const rules = rulesQ.data?.rules ?? [];
  const roles = rolesQ.data?.roles ?? [];
  const roleTitle = (id: string) => {
    const r = roles.find((x) => x.id === id);
    return r ? titles.role(r) : id;
  };

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const data = new FormData(e.currentTarget);
    const note = text(data.get("note")).trim();
    const roleId = text(data.get("role"));
    // An empty role is left out, so the server's `required` lands on the role select.
    const base = { ...(roleId ? { role_id: roleId } : {}), ...(note ? { note } : {}) } as {
      role_id: string;
      note?: string;
    };
    void create.run(async () => {
      await api.POST("/api/access/rules", {
        body:
          kind === "permission"
            ? { ...base, permission: text(data.get("permission")) }
            : { ...base, reaches: text(data.get("reaches")) as (typeof LEVELS)[number] },
      });
      void client.invalidateQueries({ queryKey: ["access", "rules"] });
      toast.show(t("core.access.rules.created"));
      formRef.current?.reset();
    });
  };

  const remove = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const r = deleting;
    if (!r) return;
    void del.run(async () => {
      await api.DELETE("/api/access/rules/{rule}", { params: { path: { rule: r.id } } });
      void client.invalidateQueries({ queryKey: ["access", "rules"] });
      toast.show(t("core.access.rules.deleted"));
      setDeleting(null);
      focusSoon("main h1");
    });
  };

  const sentence = (r: Rule) =>
    r.permission
      ? t("core.access.rules.sentence_permission", {
          role: roleTitle(r.role_id),
          permission: titles.permission(r.permission),
        })
      : t("core.access.rules.sentence_level", {
          role: roleTitle(r.role_id),
          level: titles.level(r.reaches ?? ""),
        });

  return (
    <div className="flex flex-col gap-4" aria-busy={rulesQ.isPending || undefined}>
      <h1 className="text-2xl font-bold">{t("core.access.rules.title")}</h1>
      <Tabs showRules />
      {rulesQ.isPending ? <Loading /> : null}
      {error ? <ScreenError error={error} onRetry={() => void rulesQ.refetch()} /> : null}
      {loadError && !error ? (
        <ScreenError
          error={loadError}
          onRetry={() => {
            void rolesQ.refetch();
            void catQ.refetch();
          }}
        />
      ) : null}
      {rulesQ.data && rules.length === 0 ? (
        <EmptyState>{t("core.access.rules.empty")}</EmptyState>
      ) : null}
      {rules.length > 0 ? (
        <ul className="flex flex-col gap-3" aria-label={t("core.access.rules.list")}>
          {rules.map((r) => (
            <li key={r.id} className="rounded-md border border-border bg-surface p-4">
              <p className="break-words text-lg font-semibold">{sentence(r)}</p>
              {r.note ? <p className="mt-1 break-words">{r.note}</p> : null}
              <p className="mt-1 text-muted">
                {t("core.access.rules.affected", { count: r.affected })}
              </p>
              {canManage ? (
                <div className="mt-2">
                  <Button
                    variant="danger"
                    disabled={readOnly}
                    {...(readOnly ? { "aria-describedby": "rules-readonly" } : {})}
                    onClick={() => {
                      del.clear();
                      setDeleting(r);
                    }}
                  >
                    {t("core.access.rules.delete")}
                  </Button>
                </div>
              ) : null}
            </li>
          ))}
        </ul>
      ) : null}

      {canManage && rulesQ.data ? (
        <section aria-labelledby="rules-new" className="flex flex-col gap-3">
          <h2 id="rules-new" className="text-xl font-bold">
            {t("core.access.rules.new")}
          </h2>
          <form ref={formRef} onSubmit={submit} className="flex flex-col gap-4" noValidate>
            <ErrorSummary error={create.error} formRef={formRef} />
            <Field
              label={t("core.access.rules.role")}
              name="role"
              required
              error={fieldErrors["role"]}
            >
              {(control) => (
                <select {...control} className={controlClass} defaultValue="">
                  <option value="" disabled>
                    {t("core.access.rules.choose_role")}
                  </option>
                  {roles.map((r) => (
                    <option key={r.id} value={r.id}>
                      {titles.role(r)}
                    </option>
                  ))}
                </select>
              )}
            </Field>
            <fieldset className="flex flex-col gap-1">
              <legend className="font-semibold">{t("core.access.rules.forbids")}</legend>
              <Check
                type="radio"
                name="kind"
                value="permission"
                checked={kind === "permission"}
                onChange={() => setKind("permission")}
                label={t("core.access.rules.kind_permission")}
              />
              <Check
                type="radio"
                name="kind"
                value="reaches"
                checked={kind === "reaches"}
                onChange={() => setKind("reaches")}
                label={t("core.access.rules.kind_level")}
              />
            </fieldset>
            {kind === "permission" ? (
              <Field
                label={t("core.access.rules.permission")}
                name="permission"
                required
                error={fieldErrors["permission"]}
              >
                {(control) => (
                  <select {...control} className={controlClass} defaultValue="">
                    <option value="" disabled>
                      {t("core.access.rules.choose_permission")}
                    </option>
                    {(catQ.data?.permissions ?? []).map((p) => (
                      <option key={p.name} value={p.name}>
                        {titles.permission(p.name)}
                      </option>
                    ))}
                  </select>
                )}
              </Field>
            ) : (
              <Field
                label={t("core.access.rules.level")}
                name="reaches"
                required
                error={fieldErrors["reaches"]}
              >
                {(control) => (
                  <select {...control} className={controlClass} defaultValue="personal">
                    {LEVELS.map((l) => (
                      <option key={l} value={l}>
                        {titles.level(l)}
                      </option>
                    ))}
                  </select>
                )}
              </Field>
            )}
            <Field
              label={t("core.access.rules.note")}
              name="note"
              hint={t("core.access.rules.note_hint")}
              maxLength={256}
              autoComplete="off"
              error={fieldErrors["note"]}
            />
            <div>
              <Button
                type="submit"
                busy={create.busy}
                disabled={readOnly}
                {...(readOnly ? { "aria-describedby": "rules-readonly" } : {})}
              >
                {t("core.access.rules.add")}
              </Button>
            </div>
          </form>
        </section>
      ) : null}
      <ReadOnlyNote id="rules-readonly" readOnly={readOnly} />

      <Dialog
        open={deleting !== null}
        title={t("core.access.rules.delete_title")}
        onClose={() => setDeleting(null)}
      >
        <form ref={delRef} onSubmit={remove} className="flex flex-col gap-4">
          <ErrorSummary error={del.error} formRef={delRef} />
          <p>{deleting ? sentence(deleting) : ""}</p>
          <p>{t("core.access.rules.delete_body")}</p>
          <div className="flex flex-wrap gap-3">
            <Button type="submit" variant="danger" busy={del.busy}>
              {t("core.access.rules.delete_confirm")}
            </Button>
            <Button variant="secondary" onClick={() => setDeleting(null)}>
              {t("core.access.cancel")}
            </Button>
          </div>
        </form>
      </Dialog>
    </div>
  );
}
