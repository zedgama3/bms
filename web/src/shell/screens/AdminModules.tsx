import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { api, ApiError } from "@/api";
import type { components } from "@/api";
import { isMessageKey, useFormat, useT } from "@/i18n";
import type { MessageKey } from "@/i18n";
import { EmptyState, ErrorState, Loading, ReferenceLine, Table } from "@/ui";

type AdminModules = components["schemas"]["AdminModulesResponse"];
type GraphModule = NonNullable<AdminModules["graph"]["modules"]>[number];

/** `/admin/modules`: the compiled-in modules as tables (decision 16). Needs core.modules.read;
 *  the server decides, the screen shows loading, forbidden, error, empty and success. */
export function AdminModules() {
  const t = useT();
  const q = useQuery({
    queryKey: ["core", "admin-modules"],
    queryFn: ({ signal }) => api.GET("/api/admin/modules", { signal }),
  });

  return (
    <div className="flex flex-col gap-6" aria-busy={q.isPending || undefined}>
      <h1 className="text-2xl font-bold">{t("shell.admin.modules.title")}</h1>
      {q.isPending ? <Loading /> : null}
      {q.error ? <Failure error={q.error} onRetry={() => void q.refetch()} /> : null}
      {q.data ? <Success data={q.data} /> : null}
    </div>
  );
}

function Failure({ error, onRetry }: { error: Error; onRetry: () => void }) {
  const t = useT();
  if (!(error instanceof ApiError)) throw error; // not ours: the route's crashed state
  if (error.code === "permission") {
    return (
      <div
        role="alert"
        className="flex flex-col items-start gap-3 rounded-md border border-danger bg-danger-bg p-4"
      >
        <p className="font-semibold text-danger">{t("shell.forbidden")}</p>
        {error.ref ? <ReferenceLine reference={error.ref} /> : null}
      </div>
    );
  }
  return <ErrorState error={error} onRetry={onRetry} />;
}

function Success({ data }: { data: AdminModules }) {
  const t = useT();
  const modules = data.graph.modules ?? [];
  const edges = data.graph.edges ?? [];
  const warnings = data.graph.warnings ?? [];
  return (
    <>
      <p>{t("shell.admin.modules.version", { version: data.version })}</p>
      {modules.length === 0 ? (
        <EmptyState>{t("shell.admin.modules.empty")}</EmptyState>
      ) : (
        modules.map((m) => <ModuleTable key={m.name ?? ""} module={m} schema={data.schema} />)
      )}
      {warnings.length > 0 ? (
        <section aria-labelledby="adm-warnings" className="flex flex-col gap-2">
          <h2 id="adm-warnings" className="text-xl font-bold">
            {t("shell.admin.modules.warnings")}
          </h2>
          <ul className="list-disc ps-5">
            {warnings.map((w, i) => (
              // The server's English operator text (module-contract decision 8), marked as such.
              <li key={i} lang="en">
                {w.module ? `${w.module}: ` : ""}
                {w.message}
              </li>
            ))}
          </ul>
        </section>
      ) : null}
      <section aria-labelledby="adm-edges" className="flex flex-col gap-2">
        <h2 id="adm-edges" className="text-xl font-bold">
          {t("shell.admin.modules.edges")}
        </h2>
        {edges.length === 0 ? (
          <p>{t("shell.admin.modules.edges.empty")}</p>
        ) : (
          <Table
            caption={t("shell.admin.modules.edges")}
            rows={edges}
            rowKey={(e) => `${e.from}>${e.to}:${e.kind}:${e.via}`}
            columns={[
              {
                header: t("shell.admin.modules.edges.from"),
                cell: (e) => e.from ?? "",
                rowHeader: true,
              },
              { header: t("shell.admin.modules.edges.to"), cell: (e) => e.to ?? "" },
              { header: t("shell.admin.modules.edges.kind"), cell: (e) => e.kind ?? "" },
              { header: t("shell.admin.modules.edges.via"), cell: (e) => e.via ?? "" },
            ]}
          />
        )}
      </section>
    </>
  );
}

function titleOf(t: ReturnType<typeof useT>, name: string): string {
  const key = `module.${name}.title`;
  return isMessageKey(key) ? t(key) : name;
}

function ModuleTable({
  module: m,
  schema,
}: {
  module: GraphModule;
  schema: AdminModules["schema"];
}) {
  const t = useT();
  const format = useFormat();
  const name = m.name ?? "";
  const set = schema?.find((s) => s.set === name);
  const none = t("shell.admin.modules.none");
  const list = (items: string[] | undefined): string =>
    items && items.length > 0 ? format.list(items) : none;
  const state =
    !m.has_schema || !set
      ? t("shell.admin.modules.schema.none")
      : set.current
        ? t("shell.admin.modules.schema.current", { applied: set.applied ?? 0 })
        : t("shell.admin.modules.schema.behind", {
            applied: set.applied ?? 0,
            built: set.built ?? 0,
          });
  const rows: [MessageKey, ReactNode][] = [
    [
      "shell.admin.modules.enabled",
      m.enabled ? t("shell.admin.modules.enabled") : t("shell.admin.modules.disabled"),
    ],
    ["shell.admin.modules.schema", state],
    ["shell.admin.modules.permissions", list(m.permissions)],
    ["shell.admin.modules.settings", list(m.settings)],
    ["shell.admin.modules.publishes", list(m.publishes)],
    ["shell.admin.modules.subscribes", list(m.subscribes)],
    ["shell.admin.modules.provides", list(m.provides)],
    ["shell.admin.modules.requires", list(m.requires)],
    ["shell.admin.modules.uses", list(m.uses)],
    ["shell.admin.modules.defines", list(m.defines)],
    ["shell.admin.modules.contributes_to", list(m.contributes_to)],
  ];
  return (
    <section className="flex flex-col gap-2" aria-labelledby={`adm-${name}`}>
      <h2 id={`adm-${name}`} className="text-xl font-bold">
        {titleOf(t, name)} <span className="text-base font-normal text-muted">({name})</span>
      </h2>
      <table className="w-full border-collapse text-start">
        <caption className="sr-only">{titleOf(t, name)}</caption>
        <tbody>
          {rows.map(([label, value]) => (
            <tr key={label} className="border-b border-border max-sm:block">
              <th
                scope="row"
                className="px-3 py-2 text-start font-semibold text-muted sm:w-1/3 max-sm:block"
              >
                {t(label)}
              </th>
              <td className="px-3 py-2 max-sm:block">{value}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}
