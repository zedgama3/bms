import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { createBrowserRouter } from "react-router";
import { RouterProvider } from "react-router/dom";
import type { DataRouter } from "react-router";
import { api, ApiError } from "@/api";
import { registerSessionRefresh, setSessionCheckFailed } from "@/api/internal";
import { I18nProvider, useLanguage, useT } from "@/i18n";
import type { ScreenSet } from "@/registry/types";
import { enabledSets } from "@/registry/registry";
import { Button, ErrorState, Loading } from "@/ui";
import { ToastProvider } from "@/ui/internal";
import { BootContext } from "./boot";
import { MODULES_KEY, SESSION_KEY } from "./bootKeys";
import type { SessionResult } from "./bootKeys";
import { getReporter } from "./report";
import type { BootState } from "./boot";
import { loadCatalogues } from "./catalogues";
import { buildRoutes } from "./routes";
import { parseSession, resetSession, setSession, SIGNED_OUT } from "./session";
import { SetsContext } from "./sets";

export type RouterFactory = (routes: ReturnType<typeof buildRoutes>) => DataRouter;

const browserRouter: RouterFactory = (routes) => createBrowserRouter(routes);

/** What `main.tsx` hands the application: the checked list of every compiled-in set, and a
 *  hook for the router (tests use a memory router). */
export interface AppProps {
  sets: readonly ScreenSet[];
  createRouter?: RouterFactory;
  /** Called with the router once it exists, so the client's 401 hook can navigate. */
  onRouter?: (router: DataRouter) => void;
  /** Replaces the page after sign-out; tests pass a spy. */
  leave?: (to: string) => void;
}

/** The i18n provider wraps the boot so even the cannot-reach-server state is translated. */
export function App(props: AppProps) {
  return (
    <I18nProvider>
      <ToastProvider>
        <Boot {...props} />
      </ToastProvider>
    </I18nProvider>
  );
}

function Boot({ sets, createRouter = browserRouter, onRouter, leave }: AppProps) {
  const t = useT();
  const client = useQueryClient();
  const { current: lang } = useLanguage();

  const modulesQ = useQuery({
    queryKey: MODULES_KEY,
    queryFn: ({ signal }) => api.GET("/api/modules", { signal }),
  });
  const sessionQ = useQuery({
    queryKey: SESSION_KEY,
    queryFn: async ({ signal }): Promise<SessionResult> => {
      try {
        const body = await api.GET("/api/accounts/session", { signal });
        return { identity: true, session: parseSession(body) };
      } catch (e) {
        // F28: M0 has no identity module; 404 means "signed out", not a failure.
        if (e instanceof ApiError && e.code === "not_found")
          return { identity: false, session: SIGNED_OUT };
        throw e;
      }
    },
  });

  const enabledNames = useMemo(
    () => new Set((modulesQ.data?.modules ?? []).filter((m) => m.enabled).map((m) => m.name)),
    [modulesQ.data],
  );
  const active = useMemo(() => enabledSets(sets, enabledNames), [sets, enabledNames]);
  const activeKey = active.map((s) => s.name).join(",");

  // Catalogues of the shell and of each enabled set load before the navigation shows labels.
  const [loadedFor, setLoadedFor] = useState("");
  const [catalogueFailed, setCatalogueFailed] = useState(false);
  const [catalogueAttempt, setCatalogueAttempt] = useState(0);
  useEffect(() => {
    if (!modulesQ.data) return;
    let live = true;
    const done = `${activeKey}|${lang}`;
    loadCatalogues(active, lang).then(
      () => {
        if (!live) return;
        setCatalogueFailed(false);
        setLoadedFor(done);
      },
      (e: unknown) => {
        if (!live) return;
        // The page still renders (message keys stand in for the missing text), the failure is
        // reported, and the frame offers a retry.
        getReporter()?.screenCrash(e, "/");
        setCatalogueFailed(true);
        setLoadedFor(done);
      },
    );
    return () => {
      live = false;
    };
    // `active` follows `activeKey`
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [modulesQ.data, activeKey, lang, catalogueAttempt]);
  const retryCatalogues = useCallback(() => setCatalogueAttempt((n) => n + 1), []);

  useEffect(() => {
    setSessionCheckFailed(sessionQ.isError);
  }, [sessionQ.isError]);
  useEffect(() => {
    if (sessionQ.data) setSession(sessionQ.data.session);
    else if (sessionQ.isError) resetSession();
  }, [sessionQ.data, sessionQ.isError]);

  const haveModules = modulesQ.data !== undefined;
  const router = useMemo(
    () => (haveModules ? createRouter(buildRoutes(active)) : null),
    // a new router only when the set of enabled screens changes
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [haveModules, activeKey, createRouter],
  );
  const routerRef = useRef<DataRouter | null>(null);
  useEffect(() => {
    if (router && routerRef.current !== router) {
      routerRef.current = router;
      onRouter?.(router);
    }
  }, [router, onRouter]);

  const retrySession = useCallback(() => void sessionQ.refetch(), [sessionQ]);
  const refreshSession = useCallback(async () => {
    await client.invalidateQueries({ queryKey: ["core"] });
    const fresh = client.getQueryData<SessionResult>(SESSION_KEY);
    if (fresh) setSession(fresh.session);
  }, [client]);
  useEffect(() => {
    registerSessionRefresh(refreshSession);
  }, [refreshSession]);

  const boot: BootState = {
    sessionFailed: sessionQ.isError,
    retrySession,
    identityAvailable: sessionQ.data?.identity ?? false,
    refreshSession,
    catalogueFailed,
    retryCatalogues,
    leave: leave ?? ((to) => window.location.replace(to)),
  };

  // A failed background refetch keeps the page; only a boot with no data shows the failure.
  if (modulesQ.isError && modulesQ.data === undefined) {
    const err = modulesQ.error;
    return (
      <main
        id="content"
        className="mx-auto flex min-h-dvh max-w-md flex-col justify-center gap-4 p-4"
      >
        <h1 className="text-2xl font-bold">
          {err instanceof ApiError && err.code !== "network"
            ? t("shell.crashed.title")
            : t("shell.unreachable.title")}
        </h1>
        {err instanceof ApiError ? (
          <ErrorState error={err} onRetry={() => void modulesQ.refetch()} />
        ) : (
          <Button onClick={() => void modulesQ.refetch()}>{t("shell.retry")}</Button>
        )}
      </main>
    );
  }

  const ready = router !== null && !sessionQ.isPending && loadedFor === `${activeKey}|${lang}`;
  if (!ready || router === null) {
    return (
      <main id="content" className="mx-auto min-h-dvh max-w-md p-4">
        <Loading />
      </main>
    );
  }

  return (
    <BootContext.Provider value={boot}>
      <SetsContext.Provider value={active}>
        <RouterProvider router={router} />
      </SetsContext.Provider>
    </BootContext.Provider>
  );
}
