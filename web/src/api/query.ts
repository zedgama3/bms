import { QueryClient } from "@tanstack/react-query";
import { ApiError } from "./errors";

/** Codes worth retrying a GET for (decision 11). */
const RETRYABLE = new Set(["network", "unavailable", "timeout", "shutting_down"]);

export function shouldRetry(failureCount: number, error: unknown): boolean {
  return failureCount < 2 && error instanceof ApiError && RETRYABLE.has(error.code);
}

/** 1 s, then 3 s, or Retry-After when longer. */
export function retryDelay(failureCount: number, error: unknown): number {
  const base = failureCount === 0 ? 1000 : 3000;
  const after =
    error instanceof ApiError && error.retryAfter !== undefined ? error.retryAfter * 1000 : 0;
  return Math.max(base, after);
}

/** The shell's one QueryClient: memory only (no persister), 30 s fresh, 5 min kept (decision 11). */
export function createQueryClient(overrides?: { retry?: boolean }): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 30_000,
        gcTime: 5 * 60_000,
        retry: overrides?.retry === false ? false : shouldRetry,
        retryDelay,
        refetchOnWindowFocus: true,
      },
      mutations: { retry: false },
    },
  });
}
