import { useRef, useState } from "react";
import type { FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router";
import { api, ApiError, useReadOnly } from "@/api";
import { useT } from "@/i18n";
import {
  Button,
  Check,
  controlClass,
  Dialog,
  ErrorSummary,
  Field,
  Loading,
  useFieldErrors,
  useToast,
} from "@/ui";
import { useTitles } from "../helpers";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { ScreenError } from "../ScreenError";
import { text } from "../text";
import type { PermissionInfo, Role } from "../types";
import { useAction } from "../useAction";

/** `/access/roles/new` and `/access/roles/:role`. */
export function RoleEditor() {
  const t = useT();
  const { role: id } = useParams();
  const roleQ = useQuery({
    queryKey: ["access", "role", id],
    enabled: id !== undefined,
    queryFn: ({ signal }) =>
      api.GET("/api/access/roles/{role}", { signal, params: { path: { role: id ?? "" } } }),
  });
  const catQ = useQuery({
    queryKey: ["access", "permissions"],
    queryFn: ({ signal }) => api.GET("/api/access/permissions", { signal }),
  });
  const title = id ? t("core.access.editor.title") : t("core.access.editor.new_title");
  const error =
    (roleQ.error instanceof ApiError ? roleQ.error : null) ??
    (catQ.error instanceof ApiError ? catQ.error : null);
  const ready = catQ.data && (id === undefined || roleQ.data);

  return (
    <div className="flex flex-col gap-4" aria-busy={!ready && !error ? true : undefined}>
      <h1 className="text-2xl font-bold">{title}</h1>
      <div>
        <Link to="/access/roles" className="inline-flex min-h-11 items-center underline">
          {t("core.access.editor.back")}
        </Link>
      </div>
      {!ready && !error ? <Loading /> : null}
      {error && !ready ? (
        <ScreenError
          error={error}
          onRetry={() => {
            void roleQ.refetch();
            void catQ.refetch();
          }}
        />
      ) : null}
      {ready ? (
        <EditorForm
          key={roleQ.data?.updated_at ?? "new"}
          role={roleQ.data}
          catalogue={catQ.data.permissions}
        />
      ) : null}
    </div>
  );
}

function EditorForm({ role, catalogue }: { role: Role | undefined; catalogue: PermissionInfo[] }) {
  const t = useT();
  const titles = useTitles();
  const toast = useToast();
  const navigate = useNavigate();
  const client = useQueryClient();
  const { readOnly } = useReadOnly();
  const formRef = useRef<HTMLFormElement>(null);
  const deleteRef = useRef<HTMLFormElement>(null);
  const save = useAction();
  const del = useAction();
  const [scope, setScope] = useState<"site" | "department">(role?.scope ?? "site");
  const [selected, setSelected] = useState<string[]>(role?.permissions.map((p) => p.name) ?? []);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const fieldErrors = useFieldErrors(save.error);

  const undeclared = (role?.permissions ?? []).filter((p) => !p.declared).map((p) => p.name);
  const groups = new Map<string, PermissionInfo[]>();
  for (const p of catalogue) groups.set(p.module, [...(groups.get(p.module) ?? []), p]);

  const siteOnlyInDepartment = (p: PermissionInfo) => scope === "department" && p.scope === "site";
  const toggle = (name: string, on: boolean) =>
    setSelected((cur) =>
      on ? [...cur.filter((n) => n !== name), name] : cur.filter((n) => n !== name),
    );
  const chooseScope = (next: "site" | "department") => {
    setScope(next);
    if (next === "department") {
      const siteOnly = new Set(catalogue.filter((p) => p.scope === "site").map((p) => p.name));
      setSelected((cur) => cur.filter((n) => !siteOnly.has(n)));
    }
  };
  const nameOf = (name: string) => {
    const i = selected.indexOf(name);
    return i < 0 ? "permissions" : `permissions[${i}]`;
  };

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const data = new FormData(e.currentTarget);
    const base = {
      name: text(data.get("name")).trim(),
      description: text(data.get("description")).trim(),
      permissions: selected,
    };
    void save.run(async () => {
      if (role) {
        const updated = await api.PUT("/api/access/roles/{role}", {
          params: { path: { role: role.id } },
          body: base,
        });
        client.setQueryData(["access", "role", role.id], updated);
        void client.invalidateQueries({ queryKey: ["access"] });
        toast.show(t("core.access.editor.saved"));
      } else {
        const created = await api.POST("/api/access/roles", { body: { ...base, scope } });
        void client.invalidateQueries({ queryKey: ["access"] });
        toast.show(t("core.access.editor.created"));
        void navigate(`/access/roles/${created.id}`, { replace: true });
      }
    });
  };

  const remove = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (!role) return;
    void del.run(async () => {
      await api.DELETE("/api/access/roles/{role}", { params: { path: { role: role.id } } });
      void client.invalidateQueries({ queryKey: ["access"] });
      toast.show(t("core.access.editor.deleted"));
      setConfirmDelete(false);
      void navigate("/access/roles", { replace: true });
    });
  };

  const readOnlyProps = readOnly ? { disabled: true, "aria-describedby": "role-readonly" } : {};

  return (
    <>
      {role?.key ? (
        <p className="text-muted">{t("core.access.editor.starter", { key: role.key })}</p>
      ) : null}
      {role?.base ? <p className="font-semibold">{t("core.access.editor.base")}</p> : null}
      <form ref={formRef} onSubmit={submit} className="flex flex-col gap-5" noValidate>
        <ErrorSummary error={save.error} formRef={formRef} />
        <Field
          label={t("core.access.editor.name")}
          name="name"
          required={!role?.key}
          error={fieldErrors["name"]}
        >
          {(control) => (
            <input
              {...control}
              type="text"
              maxLength={64}
              autoComplete="off"
              defaultValue={role?.name ?? ""}
              {...(role?.key ? { placeholder: titles.role(role) } : {})}
              className={controlClass}
            />
          )}
        </Field>
        <Field
          label={t("core.access.editor.description")}
          name="description"
          maxLength={512}
          autoComplete="off"
          defaultValue={role?.description ?? ""}
          error={fieldErrors["description"]}
        />
        {role ? (
          <p>
            <span className="font-semibold">{t("core.access.editor.scope")}: </span>
            {titles.scope(role.scope)}
            <span className="block text-sm text-muted">{t("core.access.editor.scope_fixed")}</span>
          </p>
        ) : (
          <fieldset className="flex flex-col gap-1">
            <legend className="font-semibold">{t("core.access.editor.scope")}</legend>
            <Check
              type="radio"
              name="scope"
              value="site"
              checked={scope === "site"}
              onChange={() => chooseScope("site")}
              label={t("core.access.scope.site")}
              hint={t("core.access.editor.scope_site_hint")}
            />
            <Check
              type="radio"
              name="scope"
              value="department"
              checked={scope === "department"}
              onChange={() => chooseScope("department")}
              label={t("core.access.scope.department")}
              hint={t("core.access.editor.scope_department_hint")}
            />
          </fieldset>
        )}

        <fieldset className="flex flex-col gap-4">
          <legend className="text-lg font-bold">{t("core.access.editor.permissions")}</legend>
          {[...groups.entries()].map(([mod, perms]) => (
            <fieldset key={mod} className="flex flex-col gap-1 rounded-md border border-border p-3">
              <legend className="px-1 font-semibold">{titles.module(mod)}</legend>
              {perms.map((p) => {
                const off = siteOnlyInDepartment(p);
                const hints = [
                  titles.scope(p.scope),
                  off ? t("core.access.editor.site_only") : "",
                ].filter(Boolean);
                return (
                  <div key={p.name} className="flex flex-col">
                    <Check
                      name={nameOf(p.name)}
                      value={p.name}
                      checked={selected.includes(p.name)}
                      disabled={off || readOnly}
                      onChange={(e) => toggle(p.name, e.target.checked)}
                      label={titles.permission(p.name)}
                      hint={hints.join(" · ")}
                    />
                    <p className="ms-14 mb-1 flex flex-wrap items-center gap-2 text-sm">
                      <code className="break-all text-muted">{p.name}</code>
                      <span
                        className={`rounded-md border px-2 py-0.5 ${
                          p.reaches === "sensitive" || p.reaches === "personal"
                            ? "border-warn-fg bg-warn-bg font-semibold text-warn-fg"
                            : "border-border"
                        }`}
                      >
                        {t("core.access.editor.reaches", { level: titles.level(p.reaches) })}
                      </span>
                    </p>
                  </div>
                );
              })}
            </fieldset>
          ))}
          {undeclared.length > 0 ? (
            <fieldset className="flex flex-col gap-2 rounded-md border border-dashed border-border p-3">
              <legend className="px-1 font-semibold">{t("core.access.editor.undeclared")}</legend>
              <p className="text-sm text-muted">{t("core.access.editor.undeclared_hint")}</p>
              {undeclared.map((name) => {
                const kept = selected.includes(name);
                return (
                  <div key={name} className="flex flex-wrap items-center gap-3">
                    <code className="min-w-0 break-all">{name}</code>
                    <span className="text-sm text-muted">
                      {kept
                        ? t("core.access.editor.not_available")
                        : t("core.access.editor.removed")}
                    </span>
                    <Button
                      variant="secondary"
                      onClick={() => toggle(name, !kept)}
                      {...readOnlyProps}
                    >
                      {kept ? t("core.access.editor.remove") : t("core.access.editor.keep")}
                    </Button>
                  </div>
                );
              })}
            </fieldset>
          ) : null}
        </fieldset>

        <ReadOnlyNote id="role-readonly" readOnly={readOnly} />
        <div className="flex flex-wrap gap-3">
          <Button type="submit" busy={save.busy} {...readOnlyProps}>
            {role ? t("core.access.editor.save") : t("core.access.editor.create")}
          </Button>
          {role && !role.base ? (
            <Button
              variant="danger"
              {...readOnlyProps}
              onClick={() => {
                del.clear();
                setConfirmDelete(true);
              }}
            >
              {t("core.access.editor.delete")}
            </Button>
          ) : null}
        </div>
      </form>

      <Dialog
        open={confirmDelete}
        title={t("core.access.editor.delete_title")}
        onClose={() => setConfirmDelete(false)}
      >
        <form ref={deleteRef} onSubmit={remove} className="flex flex-col gap-4">
          <ErrorSummary error={del.error} formRef={deleteRef} />
          <p>{t("core.access.editor.delete_body")}</p>
          <p>{t("core.access.editor.delete_in_use")}</p>
          <div className="flex flex-wrap gap-3">
            <Button type="submit" variant="danger" busy={del.busy}>
              {t("core.access.editor.delete_confirm")}
            </Button>
            <Button variant="secondary" onClick={() => setConfirmDelete(false)}>
              {t("core.access.cancel")}
            </Button>
          </div>
        </form>
      </Dialog>
    </>
  );
}
