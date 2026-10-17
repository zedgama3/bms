// The compiled-in modules' screens: one line per module (D31). The frame imports this list
// and no module folder directly.
import type { ScreenSet } from "@/registry/types";
import { ui as hello } from "./hello";

export const moduleSets: readonly ScreenSet[] = [hello];
