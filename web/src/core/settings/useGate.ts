import { useSession } from "@/api";
import { holdsSite } from "./helpers";

/** What the session shows of the settings permissions (a convenience; the server decides):
 *  `read` (the screens), `manage` (Save), and whether the person can write without reading. */
export function useGate(): { canRead: boolean; canManage: boolean } {
  const session = useSession();
  return {
    canRead: holdsSite(session, "settings.read"),
    canManage: holdsSite(session, "settings.manage"),
  };
}
