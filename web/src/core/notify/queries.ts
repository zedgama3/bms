import { useQuery } from "@tanstack/react-query";
import { api } from "@/api";

export const TEMPLATES_KEY = ["notify", "templates"] as const;
export const OUTBOX_KEY = ["notify", "outbox"] as const;
export const templateKey = (name: string, lang: string) =>
  ["notify", "template", name, lang] as const;

/** `enabled` is the session's view of the permission: without it nothing is asked. */
export function useTemplates(enabled: boolean) {
  return useQuery({
    queryKey: TEMPLATES_KEY,
    enabled,
    queryFn: ({ signal }) => api.GET("/api/notify/templates", { signal }),
  });
}

export function useTemplate(name: string, lang: string, enabled: boolean) {
  return useQuery({
    queryKey: templateKey(name, lang),
    enabled,
    queryFn: ({ signal }) =>
      api.GET("/api/notify/templates/{name}/{lang}", {
        signal,
        params: { path: { name, lang } },
      }),
  });
}

export function useOutbox(enabled: boolean) {
  return useQuery({
    queryKey: OUTBOX_KEY,
    enabled,
    queryFn: ({ signal }) => api.GET("/api/notify/outbox", { signal }),
  });
}
