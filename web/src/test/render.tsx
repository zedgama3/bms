import { render } from "@testing-library/react";
import type { RenderResult } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import type { ReactElement } from "react";
import { createQueryClient } from "@/api/internal";
import { I18nProvider } from "@/i18n";
import { addCatalogue, configureLanguages } from "@/i18n/internal";
import shellEn from "@/shell/messages/en.json";
import helloEn from "@/modules/hello/messages/en.json";
import { ToastProvider } from "@/ui/internal";

/** Loads the shell's (and hello's) English catalogue, plus any extra languages given. */
export function setupI18n(
  extra: Record<string, Record<string, string>> = {},
  langs: string[] = ["en"],
): void {
  configureLanguages(["en", ...Object.keys(extra), ...langs.filter((l) => l !== "en")], "en");
  addCatalogue("shell", "en", shellEn);
  addCatalogue("hello", "en", helloEn);
  for (const [lang, messages] of Object.entries(extra)) addCatalogue("shell", lang, messages);
}

export interface RenderOptions {
  client?: QueryClient;
  /** Extra English messages (a module's `module.<name>.field.<code>` keys, for example). */
  messages?: Record<string, string>;
}

/** Renders inside the i18n, query and toast providers (no router). */
export function renderUi(
  ui: ReactElement,
  opts: RenderOptions = {},
): RenderResult & { client: QueryClient } {
  setupI18n();
  if (opts.messages) addCatalogue("test", "en", opts.messages);
  const client = opts.client ?? createQueryClient({ retry: false });
  const wrap = (node: ReactElement) => (
    <QueryClientProvider client={client}>
      <I18nProvider>
        <ToastProvider>{node}</ToastProvider>
      </I18nProvider>
    </QueryClientProvider>
  );
  const result = render(wrap(ui));
  const inner = result.rerender;
  return Object.assign(result, {
    client,
    rerender: (next: ReactElement) => inner(wrap(next)),
  });
}
