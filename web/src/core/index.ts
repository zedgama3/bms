// The core areas' screens (accounts, profile, people, ...): one line per area (D31). The
// shell's own screens (home, admin modules) live in src/shell.
import type { ScreenSet } from "@/registry/types";
import { ui as accounts } from "./accounts";

export const coreSets: readonly ScreenSet[] = [accounts];
