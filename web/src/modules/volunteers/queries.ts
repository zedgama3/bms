import { useQuery } from "@tanstack/react-query";
import { core, vol } from "./calls";
import type { Department, EventRef, Position, Qualification } from "./types";

/** Active departments (names label shifts and cards). */
export function useDepartments() {
  return useQuery({
    queryKey: ["volunteers", "departments"],
    queryFn: ({ signal }) => core.departments({ signal }),
  });
}

export function useEvents() {
  return useQuery({
    queryKey: ["volunteers", "events"],
    queryFn: ({ signal }) => core.events({ signal }),
  });
}

/** The catalogue the caller may see (org-wide and every department's active entries). */
export function useQualifications(enabled = true) {
  return useQuery({
    queryKey: ["volunteers", "qualifications", "all"],
    enabled,
    queryFn: ({ signal }) => vol.qualifications({}, { signal }),
  });
}

export function usePositions(department: string, enabled = true) {
  return useQuery({
    queryKey: ["volunteers", "positions", department],
    enabled: enabled && department !== "",
    queryFn: ({ signal }) => vol.positions(department, false, { signal }),
  });
}

/** Name lookups from a possibly-unloaded list: a missing name is "" (the caller falls back). */
export function nameIn(
  list: readonly (Department | EventRef | Position | Qualification)[] | undefined,
  id: string | undefined,
): string {
  if (!id) return "";
  return list?.find((x) => x.id === id)?.name ?? "";
}
