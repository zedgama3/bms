import { useQuery } from "@tanstack/react-query";
import { api } from "@/api";

export const JOBS_KEY = ["jobs", "status"] as const;

/** `enabled` is the session's view of `jobs.read`: without it nothing is asked. */
export function useJobsStatus(enabled: boolean) {
  return useQuery({
    queryKey: JOBS_KEY,
    enabled,
    queryFn: ({ signal }) => api.GET("/api/jobs/", { signal }),
  });
}
