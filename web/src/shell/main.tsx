import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import type { DataRouter } from "react-router";
import { createQueryClient } from "@/api/internal";
import { addCatalogue, configureLanguages } from "@/i18n/internal";
import { coreSets } from "@/core";
import { moduleSets } from "@/modules";
import { validateRegistry, RegistryError } from "@/registry/registry";
import { setUntranslatedHandler } from "@/ui/internal";
import "../index.css";
import { App } from "./App";
import { shellLanguages, shellMessages } from "./catalogues";
import { DEFAULT_LANGUAGE } from "./regional";
import { installReporter } from "./report";
import { installUsage } from "./usage";
import { wireClient } from "./wire";

// 1. The reporter first: a crash during boot is the kind a maintainer never hears about.
const reporter = installReporter({ appVersion: __BMS_VERSION__ });

// Languages and <html lang> (decision 7): the regional's default, then the stored choice.
configureLanguages(shellLanguages, DEFAULT_LANGUAGE);
setUntranslatedHandler((what, code, error) => reporter.untranslated(what, code, error));

const queryClient = createQueryClient();
let router: DataRouter | null = null;

// 3. The client's hooks.
wireClient({ reporter, queryClient, getRouter: () => router });

const rootEl = document.getElementById("root");

async function boot(): Promise<void> {
  if (!rootEl) return;
  try {
    // 2. The registry: a bad set is a crashed screen, not a half-built page.
    const sets = validateRegistry([...coreSets, ...moduleSets]);
    // 2b. Usage counts (D54), after the reporter and the registry (it needs the declared
    // actions); none at all under Global Privacy Control or Do Not Track.
    installUsage({
      appVersion: __BMS_VERSION__,
      actions: new Set(sets.flatMap((s) => s.actions ?? [])),
    });
    // The shell's own English catalogue is needed even by the very first loading state.
    addCatalogue("shell", "en", await shellMessages["en"]!());
    createRoot(rootEl).render(
      <StrictMode>
        <QueryClientProvider client={queryClient}>
          <App
            sets={sets}
            onRouter={(r) => {
              router = r;
            }}
          />
        </QueryClientProvider>
      </StrictMode>,
    );
  } catch (e) {
    reporter.screenCrash(e, "/");
    rootEl.textContent = e instanceof RegistryError ? e.message : "Something went wrong.";
  }
}

void boot();
