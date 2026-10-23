import { useQuery } from "@tanstack/react-query";
import { api } from "@/api";

/** Playa names for person IDs, in one call (`GET /api/people/names`). Memory only. Until the
 *  answer arrives, or if it fails, a person shows as `fallback`: a name is a convenience, not
 *  a reason to fail the screen. */
export function useNames(ids: readonly string[], fallback: string): (id: string) => string {
  const unique = [...new Set(ids.filter((i) => i !== ""))].sort().slice(0, 200);
  const q = useQuery({
    queryKey: ["volunteers", "names", unique.join(",")],
    enabled: unique.length > 0,
    queryFn: ({ signal }) =>
      api.GET("/api/people/names", { signal, params: { query: { ids: unique.join(",") } } }),
  });
  const names = q.data?.names ?? {};
  return (id) => names[id] || fallback;
}
