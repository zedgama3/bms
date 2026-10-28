import { useAction as useBaseAction } from "@/api";
import { withRangeCodes } from "./rangeCodes";

/** The shared `useAction` with this module's `range` / `range_order` field codes applied to
 *  every error it keeps. */
export function useAction() {
  return useBaseAction(withRangeCodes);
}
