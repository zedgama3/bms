import { createContext, useContext } from "react";
import type { ScreenSet } from "@/registry/types";

/** The enabled sets, for the navigation and the home cards. */
export const SetsContext = createContext<readonly ScreenSet[]>([]);

export function useEnabledSets(): readonly ScreenSet[] {
  return useContext(SetsContext);
}
