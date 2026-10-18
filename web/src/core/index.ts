// The core areas' screens (accounts, profile, people, ...): one line per area (D31). The
// shell's own screens (home, admin modules) live in src/shell.
import type { ScreenSet } from "@/registry/types";
import { ui as access } from "./access";
import { ui as accounts } from "./accounts";
import { ui as audit } from "./audit";
import { ui as people } from "./people";

export const coreSets: readonly ScreenSet[] = [accounts, access, people, audit];
