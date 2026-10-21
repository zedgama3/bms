import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback } from "react";
import { api } from "@/api";

export const DOCUMENT_KEY = ["settings", "document"] as const;
export const TERMS_KEY = ["settings", "terms"] as const;
/** The frame's boot query for GET /api/settings/public (web-shell "Settings"): invalidating it
 *  makes the frame fetch the document again and apply the new name and terms at once. */
const PUBLIC_KEY = ["core", "settings", "public"] as const;

/** The admin document (`settings.read`). `enabled` is the session's view of the permission:
 *  without it nothing is asked. */
export function useSettingsDocument(enabled: boolean) {
  return useQuery({
    queryKey: DOCUMENT_KEY,
    enabled,
    queryFn: ({ signal }) => api.GET("/api/settings/", { signal }),
  });
}

/** The terminology document (`settings.read`). */
export function useTermsDocument(enabled: boolean) {
  return useQuery({
    queryKey: TERMS_KEY,
    enabled,
    queryFn: ({ signal }) => api.GET("/api/settings/terms", { signal }),
  });
}

/** After a save: refetch the document the form reads (resolves when it has), and have the frame
 *  re-read the public document, so the admin sees the change without a reload. */
export function useAfterSave() {
  const client = useQueryClient();
  return useCallback(
    async (key: readonly unknown[]) => {
      void client.invalidateQueries({ queryKey: PUBLIC_KEY });
      await client.invalidateQueries({ queryKey: key });
    },
    [client],
  );
}

/** After a logo change: the frame re-reads the public document (resolves when it has), so the
 *  header and the preview show the new logo without a reload. */
export function useRefreshPublic() {
  const client = useQueryClient();
  return useCallback(() => client.invalidateQueries({ queryKey: PUBLIC_KEY }), [client]);
}
